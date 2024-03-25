package throttler

import (
	"fmt"
	"strconv"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/sirupsen/logrus"
)

func NewConcurrencyControllerBasedOnIOLoad(maxIOLoad string, close chan struct{}) *ConcurrencyController {
	currentLoad := 0.0
	var err error
	err = nil

	maxIOLoadf, err := strconv.ParseFloat(maxIOLoad, 64)
	if err != nil {
		logrus.Fatalf("failed to parse maxCpuLoad: %s", maxIOLoad)
	}

	c, updated := NewConcurrencyControllerWithDynamicCondition(func(int) (bool, error) { return currentLoad < maxIOLoadf, err }, fmt.Sprintf("current IO Load < %s", maxIOLoad))

	go func() {
		for {
			select {
			case <-close:
				logrus.Info("closing IO load monitor")
				err = fmt.Errorf("closing IO load monitor")
				return
			default:
				currentLoad = GetIoWait()
				updated()
				logrus.Debugf("current IO load: %f", currentLoad)
			}
		}
	}()
	return c
}

func GetIoWait() float64 {
	perCPU := false // get total load
	loadFirst, err := cpu.Times(perCPU)
	if err != nil {
		return 0
	}

	time.Sleep(5 * time.Second)
	loadSecond, err := cpu.Times(perCPU)
	if err != nil {
		return 0
	}
	averageLoad := (loadSecond[0].Iowait - loadFirst[0].Iowait) / 10

	return averageLoad
}
