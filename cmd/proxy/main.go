package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/splitio/split-synchronizer/v4/splitio"
	"github.com/splitio/split-synchronizer/v4/splitio/common"
	cconf "github.com/splitio/split-synchronizer/v4/splitio/common/conf"
	"github.com/splitio/split-synchronizer/v4/splitio/log"
	"github.com/splitio/split-synchronizer/v4/splitio/proxy"
	"github.com/splitio/split-synchronizer/v4/splitio/proxy/conf"
)

const (
	exitCodeSuccess     = 0
	exitCodeConfigError = 1
)

func parseCliArgs() *cconf.CliFlags {
	return cconf.ParseCliArgs(&conf.Main{})
}

func setupConfig(cliArgs *cconf.CliFlags) (*conf.Main, error) {
	proxyConf := conf.Main{}
	cconf.PopulateDefaults(&proxyConf)

	if path := *cliArgs.ConfigFile; path != "" {
		err := cconf.PopulateConfigFromFile(path, &proxyConf)
		if err != nil {
			return nil, fmt.Errorf("error parsing config file: %w", err)
		}
	}

	cconf.PopulateFromArguments(&proxyConf, cliArgs.RawConfig)
	return &proxyConf, nil
}

func main() {
	fmt.Println(splitio.ASCILogo)
	fmt.Printf("\nSplit Proxy - Version: %s (%s) \n", splitio.Version, splitio.CommitVersion)

	cliArgs := parseCliArgs()
	if *cliArgs.VersionInfo { //already printed, we can now exit
		os.Exit(exitCodeSuccess)
	}

	if fn := *cliArgs.WriteDefaultConfigFile; fn != "" {
		if err := cconf.WriteDefaultConfigFile(fn, &conf.Main{}); err != nil {
			fmt.Printf("error writing config file with default values: %s", err.Error())
			os.Exit(exitCodeConfigError)
		}
		os.Exit(exitCodeSuccess)
	}

	cfg, err := setupConfig(cliArgs)
	if err != nil {
		fmt.Println("error processing config: ", err)
		os.Exit(exitCodeConfigError)
	}

	logger := log.BuildFromConfig(&cfg.Logging, "Split-Proxy", &cfg.Integrations.Slack)
	err = proxy.Start(logger, cfg)

	if err == nil {
		return
	}

	var initError *common.InitializationError
	if errors.As(err, &initError) {
		logger.Error("Failed to initialize the split sync: ", initError)
		os.Exit(initError.ExitCode())
	}

	os.Exit(common.ExitUndefined)
}
