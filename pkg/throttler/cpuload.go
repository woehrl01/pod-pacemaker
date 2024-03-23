package throttler

import (
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
)

func NewConcurrencyControllerBasedOnCpu(maxCpuLoad float64) *ConcurrencyController {
	currentLoad := 0.0
	go func() {
		for {
			currentLoad = GetCpuLoad()
		}
	}()
	return NewConcurrencyControllerWithDynamicCondition(func(int) bool { return currentLoad < maxCpuLoad })
}

func GetCpuLoad() float64 {
	perCpu := false // get total load
	load, err := cpu.Percent(10*time.Second, perCpu)
	if err != nil {
		return 0
	}
	return load[0]
}
