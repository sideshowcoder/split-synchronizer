package counter

import (
	"testing"

	"github.com/splitio/go-toolkit/logging"
)

func TestPeriodicCounter(t *testing.T) {

	counter := NewCounterPeriodic(Config{
		Name:                     "Test",
		CounterType:              0,
		Periodic:                 true,
		TaskFunc:                 func(l logging.LoggerInterface, c BaseCounterInterface) error { return nil },
		Period:                   2,
		MaxErrorsAllowedInPeriod: 2,
		Severity:                 0,
	}, logging.NewLogger(nil))

	counter.Start()

	res := counter.IsHealthy()
	if !res {
		t.Errorf("Healthy should be true")
	}

	counter.NotifyEvent()
	res = counter.IsHealthy()
	if !res {
		t.Errorf("Healthy should be true")
	}

	counter.Reset(0)
	res = counter.IsHealthy()
	if !res {
		t.Errorf("Healthy should be true")
	}

	count := counter.GetErrorsCount()
	if *count != 0 {
		t.Errorf("Errors should be 0")
	}

	counter.NotifyEvent()
	counter.NotifyEvent()
	res = counter.IsHealthy()
	if res {
		t.Errorf("Healthy should be false")
	}

	count = counter.GetErrorsCount()
	if *count != 2 {
		t.Errorf("Errors should be 2")
	}

	counter.Stop()
}
