package throttler

import (
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/sirupsen/logrus"
)

func NewConcurrencyControllerBasedOnCpu(maxCpuLoad float64, close chan struct{}) *ConcurrencyController {
	currentLoad := 0.0
	go func() {
		for {
			select {
			case <-close:
				logrus.Info("closing cpu load monitor")
				return
			default:
				currentLoad = GetCpuLoad()
				logrus.Debugf("current cpu load: %f", currentLoad)
			}
		}
	}()
	return NewConcurrencyControllerWithDynamicCondition(func(int) bool { return currentLoad < maxCpuLoad }, fmt.Sprintf("currentCpuLoad < %f", maxCpuLoad))
}

func GetCpuLoad() float64 {
	perCpu := false // get total load
	load, err := cpu.Percent(5*time.Second, perCpu)
	if err != nil {
		return 0
	}
	return load[0]
}
