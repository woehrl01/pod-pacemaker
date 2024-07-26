package throttler

import (
	"fmt"
	"strconv"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/sirupsen/logrus"
)

func NewConcurrencyControllerBasedOnLoadAvg(maxLoadAvg string, perCpu bool, incrementByStr string, close chan struct{}) *ConcurrencyController {
	currentLoad := 0.0
	var err error
	err = nil

	maxLoadAvgF, err := strconv.ParseFloat(maxLoadAvg, 64)
	if err != nil {
		logrus.Fatalf("failed to parse maxCpuLoad: %s", maxLoadAvg)
	}

	incrementBy := 0.0
	if incrementByStr != "" {
		incrementBy, err = strconv.ParseFloat(incrementByStr, 64)
		if err != nil {
			logrus.Fatalf("failed to parse incrementBy: %s", incrementByStr)
		}
	}

	c, updated := NewConcurrencyControllerWithDynamicCondition(&DynamicOptions{
		Condition:    func(i int) (bool, error) { return currentLoad < maxLoadAvgF, err },
		OnAquire:     func() { currentLoad += incrementBy },
		ConditionStr: fmt.Sprintf("current Load Avg < %s", maxLoadAvg),
	})

	go func() {
		for {
			select {
			case <-close:
				logrus.Info("closing load avg monitor")
				err = fmt.Errorf("closing load avg monitor")
				return
			default:
				currentLoad = GetLoadAvg(perCpu)
				updated()
				logrus.Debugf("current load avg: %f", currentLoad)
				time.Sleep(5 * time.Second)
			}
		}
	}()
	return c
}

// GetLoadAvg returns the current load average of the system
func GetLoadAvg(perCpu bool) float64 {
	load, err := load.Avg()
	if err != nil {
		return 0
	}

	if perCpu {
		cpuCount, err := cpu.Counts(true)
		if err != nil {
			return 0
		}

		return load.Load1 / float64(cpuCount)
	}

	return load.Load1
}
