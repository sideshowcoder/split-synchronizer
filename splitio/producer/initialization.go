package producer

import (
	"errors"
	"fmt"
	"time"

	cconf "github.com/splitio/go-split-commons/v4/conf"
	"github.com/splitio/go-split-commons/v4/dtos"
	"github.com/splitio/go-split-commons/v4/provisional"
	"github.com/splitio/go-split-commons/v4/service/api"
	"github.com/splitio/go-split-commons/v4/telemetry"

	"github.com/splitio/go-split-commons/v4/healthcheck/application"
	"github.com/splitio/go-split-commons/v4/storage/inmemory"
	"github.com/splitio/go-split-commons/v4/storage/redis"
	"github.com/splitio/go-split-commons/v4/synchronizer"
	"github.com/splitio/go-split-commons/v4/synchronizer/worker/impressionscount"
	"github.com/splitio/go-split-commons/v4/synchronizer/worker/segment"
	"github.com/splitio/go-split-commons/v4/synchronizer/worker/split"
	"github.com/splitio/go-split-commons/v4/tasks"
	"github.com/splitio/go-toolkit/v5/logging"

	"github.com/splitio/split-synchronizer/v4/splitio/admin"
	adminCommon "github.com/splitio/split-synchronizer/v4/splitio/admin/common"
	"github.com/splitio/split-synchronizer/v4/splitio/common"
	"github.com/splitio/split-synchronizer/v4/splitio/common/impressionlistener"
	ssync "github.com/splitio/split-synchronizer/v4/splitio/common/sync"

	"github.com/splitio/split-synchronizer/v4/splitio/producer/conf"
	"github.com/splitio/split-synchronizer/v4/splitio/producer/evcalc"
	"github.com/splitio/split-synchronizer/v4/splitio/producer/storage"
	"github.com/splitio/split-synchronizer/v4/splitio/producer/task"
	"github.com/splitio/split-synchronizer/v4/splitio/producer/worker"
	"github.com/splitio/split-synchronizer/v4/splitio/util"
)

// Start initialize the producer mode
func Start(logger logging.LoggerInterface, cfg *conf.Main) error {
	// Getting initial config data
	advanced := cfg.BuildAdvancedConfig()
	metadata := util.GetMetadata(false, cfg.IPAddressEnabled)

	clientKey, err := util.GetClientKey(cfg.Apikey)
	if err != nil {
		return common.NewInitError(fmt.Errorf("error parsing client key from provided apikey: %w", err), common.ExitInvalidApikey)
	}

	// Setup fetchers & recorders
	splitAPI := api.NewSplitAPI(cfg.Apikey, *advanced, logger, metadata)

	// Check if apikey is valid
	if !isValidApikey(splitAPI.SplitFetcher) {
		return common.NewInitError(errors.New("invalid apikey"), common.ExitInvalidApikey)
	}

	// Redis Storages
	redisOptions, err := parseRedisOptions(&cfg.Storage.Redis)
	if err != nil {
		return common.NewInitError(fmt.Errorf("error parsing redis config: %w", err), common.ExitRedisInitializationFailed)
	}
	redisClient, err := redis.NewRedisClient(redisOptions, logger)
	if err != nil {
		// THIS BRANCH WILL CURRENTLY NEVER BE REACHED
		// TODO(mredolatti/mmelograno): Currently the commons library panics if the redis server is unreachable.
		// this behaviour should be revisited since this might bring down a client app if called from the sdk
		return common.NewInitError(fmt.Errorf("error instantiating redis client: %w", err), common.ExitRedisInitializationFailed)
	}

	// Instantiating storages
	miscStorage := redis.NewMiscStorage(redisClient, logger)
	err = sanitizeRedis(cfg, miscStorage, logger)
	if err != nil {
		return common.NewInitError(fmt.Errorf("error cleaning up redis: %w", err), common.ExitRedisInitializationFailed)
	}

	// Handle dual telemetry:
	// - telemetry generated by split-sync
	// - telemetry generated by sdks and picked up by split-sync
	syncTelemetryStorage, _ := inmemory.NewTelemetryStorage()
	sdkTelemetryStorage := storage.NewRedisTelemetryCosumerclient(redisClient, logger)

	// These storages are forwarded to the dashboard, the sdk-telemetry is irrelevant there
	storages := adminCommon.Storages{
		SplitStorage:          redis.NewSplitStorage(redisClient, logger),
		SegmentStorage:        redis.NewSegmentStorage(redisClient, logger),
		LocalTelemetryStorage: syncTelemetryStorage,
		ImpressionStorage:     redis.NewImpressionStorage(redisClient, dtos.Metadata{}, logger),
		EventStorage:          redis.NewEventsStorage(redisClient, dtos.Metadata{}, logger),
	}

	// Creating Workers and Tasks
	eventEvictionMonitor := evcalc.New(1) // TODO(mredolatti): set the correct thread count
	eventRecorder := worker.NewEventRecorderMultiple(storages.EventStorage, splitAPI.EventRecorder, syncTelemetryStorage, eventEvictionMonitor, logger)

	workers := synchronizer.Workers{
		SplitFetcher: split.NewSplitFetcher(storages.SplitStorage, splitAPI.SplitFetcher, logger, syncTelemetryStorage, &application.Dummy{}),
		SegmentFetcher: segment.NewSegmentFetcher(storages.SplitStorage, storages.SegmentStorage, splitAPI.SegmentFetcher,
			logger, syncTelemetryStorage, &application.Dummy{}),
		EventRecorder: eventRecorder,
		// local telemetry
		TelemetryRecorder: telemetry.NewTelemetrySynchronizer(syncTelemetryStorage, splitAPI.TelemetryRecorder,
			storages.SplitStorage, storages.SegmentStorage, logger, metadata, syncTelemetryStorage),
	}
	splitTasks := synchronizer.SplitTasks{
		SplitSyncTask: tasks.NewFetchSplitsTask(workers.SplitFetcher, int(cfg.Sync.SplitRefreshRateMs)/1000, logger),
		SegmentSyncTask: tasks.NewFetchSegmentsTask(workers.SegmentFetcher, int(cfg.Sync.SegmentRefreshRateMs)/1000,
			advanced.SegmentWorkers, advanced.SegmentQueueSize, logger),
		// local telemetry
		TelemetrySyncTask: tasks.NewRecordTelemetryTask(workers.TelemetryRecorder, int(cfg.Sync.Advanced.InternalMetricsRateMs)/1000, logger),
		EventSyncTask: tasks.NewRecordEventsTasks(workers.EventRecorder, advanced.EventsBulkSize, 5, // TODO: see if we remove this
			logger, 1), // TODO:See if we remove this
	}

	impressionEvictionMonitor := evcalc.New(1) // TODO(mredolatti): set the correct thread count
	var impListener impressionlistener.ImpressionBulkListener
	if cfg.Integrations.ImpressionListener.Endpoint != "" {
		// TODO(mredolatti): make the listener queue size configurable
		var err error
		impListener, err = impressionlistener.NewImpressionBulkListener(
			cfg.Integrations.ImpressionListener.Endpoint,
			int(cfg.Integrations.ImpressionListener.QueueSize),
			nil)
		if err != nil {
			return common.NewInitError(fmt.Errorf("error instantiating impression listener: %w", err), common.ExitTaskInitialization)
		}
		impListener.Start()

	}

	managerConfig := cconf.ManagerConfig{
		ImpressionsMode: cfg.Sync.ImpressionsMode,
		OperationMode:   cconf.ProducerSync,
		ListenerEnabled: impListener != nil,
	}

	var impressionsCounter *provisional.ImpressionsCounter
	if cfg.Sync.ImpressionsMode == cconf.ImpressionsModeOptimized {
		impressionsCounter = provisional.NewImpressionsCounter()
		workers.ImpressionsCountRecorder = impressionscount.NewRecorderSingle(impressionsCounter, splitAPI.ImpressionRecorder, metadata,
			logger, syncTelemetryStorage)
		splitTasks.ImpressionsCountSyncTask = tasks.NewRecordImpressionsCountTask(workers.ImpressionsCountRecorder, logger)
	}
	impressionRecorder, err := worker.NewImpressionRecordMultiple(storages.ImpressionStorage, splitAPI.ImpressionRecorder, impListener,
		syncTelemetryStorage, logger, managerConfig, impressionsCounter, impressionEvictionMonitor)
	if err != nil {
		return common.NewInitError(fmt.Errorf("error instantiating impression recorder: %w", err), common.ExitTaskInitialization)
	}
	splitTasks.ImpressionSyncTask = tasks.NewRecordImpressionsTasks(impressionRecorder, 5, logger, // TODO: set appropriate impressions rate
		advanced.ImpressionsBulkSize, cfg.Sync.Advanced.ImpressionsPostConcurrency)

	sdkTelemetryWorker := worker.NewTelemetryMultiWorker(logger, sdkTelemetryStorage, splitAPI.TelemetryRecorder)
	sdkTelemetryTask := task.NewTelemetrySyncTask(sdkTelemetryWorker, logger, 10)
	syncImpl := ssync.NewSynchronizer(*advanced, splitTasks, workers, logger, nil, []tasks.Task{sdkTelemetryTask})
	managerStatus := make(chan int, 1)
	syncManager, err := synchronizer.NewSynchronizerManager(
		syncImpl,
		logger,
		*advanced,
		splitAPI.AuthClient,
		storages.SplitStorage,
		managerStatus,
		syncTelemetryStorage,
		metadata,
		&clientKey,
		&application.Dummy{},
	)

	if err != nil {
		return common.NewInitError(fmt.Errorf("error instantiating sync manager: %w", err), common.ExitTaskInitialization)
	}

	rtm := common.NewRuntime(false, syncManager, logger, "Split Synchronizer", nil, nil)

	// --------------------------- ADMIN DASHBOARD ------------------------------
	adminServer, err := admin.NewServer(&admin.Options{
		Host:                cfg.Admin.Host,
		Port:                int(cfg.Admin.Port),
		Name:                "Split Synchronizer dashboard (producer mode)",
		Proxy:               false,
		Username:            cfg.Admin.Username,
		Password:            cfg.Admin.Password,
		Logger:              logger,
		Storages:            storages,
		ImpressionsEvCalc:   impressionEvictionMonitor,
		ImpressionsRecorder: impressionRecorder,
		EventRecorder:       eventRecorder,
		EventsEvCalc:        eventEvictionMonitor,
		Runtime:             rtm,
	})
	if err != nil {
		panic(err.Error())
	}
	go adminServer.ListenAndServe()

	// Run Sync Manager
	before := time.Now()
	go syncManager.Start()
	select {
	case status := <-managerStatus:
		switch status {
		case synchronizer.Ready:
			logger.Info("Synchronizer tasks started")
			workers.TelemetryRecorder.SynchronizeConfig(
				telemetry.InitConfig{
					AdvancedConfig: *advanced,
					TaskPeriods: cconf.TaskPeriods{
						SplitSync:     int(cfg.Sync.SplitRefreshRateMs / 1000),
						SegmentSync:   int(cfg.Sync.SegmentRefreshRateMs / 1000),
						TelemetrySync: int(cfg.Sync.Advanced.InternalMetricsRateMs / 1000),
					},
					ManagerConfig: managerConfig,
				},
				time.Now().Sub(before).Milliseconds(),
				map[string]int64{cfg.Apikey: 1},
				nil,
			)
		case synchronizer.Error:
			logger.Error("Initial synchronization failed. Either split is unreachable or the APIKey is incorrect. Aborting execution.")
			return common.NewInitError(fmt.Errorf("error instantiating sync manager: %w", err), common.ExitTaskInitialization)
		}
	}

	rtm.RegisterShutdownHandler()
	rtm.Block()
	return nil
}
