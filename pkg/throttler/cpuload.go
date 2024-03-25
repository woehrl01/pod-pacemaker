package throttler

import (
	"fmt"
	"strconv"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/sirupsen/logrus"
)

func NewConcurrencyControllerBasedOnCpu(maxCpuLoad string, incrementByStr string, close chan struct{}) *ConcurrencyController {
	currentLoad := 0.0
	var err error
	err = nil

	maxCpuLoadf, err := strconv.ParseFloat(maxCpuLoad, 64)
	if err != nil {
		logrus.Fatalf("failed to parse maxCpuLoad: %s", maxCpuLoad)
	}

	incrementBy := 0.0
	if incrementByStr != "" {
		incrementBy, err = strconv.ParseFloat(incrementByStr, 64)
		if err != nil {
			logrus.Fatalf("failed to parse incrementBy: %s", incrementByStr)
		}
	}

	c, updated := NewConcurrencyControllerWithDynamicCondition(&DynamicOptions{
		Condition:    func(i int) (bool, error) { return currentLoad < maxCpuLoadf, err },
		OnAquire:     func() { currentLoad += incrementBy },
		ConditionStr: fmt.Sprintf("current CPU Load < %s", maxCpuLoad),
	})

	go func() {
		for {
			select {
			case <-close:
				logrus.Info("closing cpu load monitor")
				err = fmt.Errorf("closing cpu load monitor")
				return
			default:
				currentLoad = GetCpuLoad()
				updated()
				logrus.Debugf("current cpu load: %f", currentLoad)
			}
		}
	}()
	return c
}

func GetCpuLoad() float64 {
	perCpu := false // get total load
	load, err := cpu.Percent(5*time.Second, perCpu)
	if err != nil {
		return 0
	}
	return load[0]
}
