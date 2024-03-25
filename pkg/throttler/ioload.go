package throttler

import (
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/sirupsen/logrus"
)

func NewConcurrencyControllerBasedOnIOLoad(maxIOLoad float64, close chan struct{}) *ConcurrencyController {
	currentLoad := 0.0
	go func() {
		for {
			select {
			case <-close:
				logrus.Info("closing IO load monitor")
				return
			default:
				currentLoad = GetIoWait()
				logrus.Debugf("current IO load: %f", currentLoad)
			}
		}
	}()
	return NewConcurrencyControllerWithDynamicCondition(func(int) bool { return currentLoad < maxIOLoad }, fmt.Sprintf("current IO Load < %f", maxIOLoad))
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
