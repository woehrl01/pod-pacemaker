package throttler

import (
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
)

func NewConcurrencyControllerBasedOnIOLoad(maxIOLoad float64) *ConcurrencyController {
	currentLoad := 0.0
	go func() {
		for {
			currentLoad = GetIoWait()
		}
	}()
	return NewConcurrencyControllerWithDynamicCondition(func(int) bool { return currentLoad < maxIOLoad })
}

func GetIoWait() float64 {
	perCPU := false // get total load
	loadFirst, err := cpu.Times(perCPU)
	if err != nil {
		return 0
	}

	time.Sleep(10 * time.Second)
	loadSecond, err := cpu.Times(perCPU)
	if err != nil {
		return 0
	}
	averageLoad := (loadSecond[0].Iowait - loadFirst[0].Iowait) / 10

	return averageLoad
}
