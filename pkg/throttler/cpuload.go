package throttler

import (
	"fmt"
	"strconv"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/sirupsen/logrus"
)

func NewConcurrencyControllerBasedOnCpu(maxCpuLoad string, close chan struct{}) *ConcurrencyController {
	currentLoad := 0.0
	var err error
	err = nil

	maxCpuLoadf, err := strconv.ParseFloat(maxCpuLoad, 64)
	if err != nil {
		logrus.Fatalf("failed to parse maxCpuLoad: %s", maxCpuLoad)
	}

	c, updated := NewConcurrencyControllerWithDynamicCondition(func(int) (bool, error) { return currentLoad < maxCpuLoadf, err }, fmt.Sprintf("currentCpuLoad < %s", maxCpuLoad))

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
