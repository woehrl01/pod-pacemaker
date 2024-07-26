package throttler

import (
	"fmt"
	"strconv"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/sirupsen/logrus"
)

func NewConcurrencyControllerBasedOnIOLoad(maxIOLoad string, incrementByStr string, close chan struct{}) *ConcurrencyController {
	currentLoad := 0.0
	var err error
	err = nil

	maxIOLoadf, err := strconv.ParseFloat(maxIOLoad, 64)
	if err != nil {
		logrus.Fatalf("failed to parse maxCpuLoad: %s", maxIOLoad)
	}

	incrementBy := 0.0
	if incrementByStr != "" {
		incrementBy, err = strconv.ParseFloat(incrementByStr, 64)
		if err != nil {
			logrus.Fatalf("failed to parse incrementBy: %s", incrementByStr)
		}
	}

	c, updated := NewConcurrencyControllerWithDynamicCondition(&DynamicOptions{
		Condition:    func(i int) (bool, error) { return currentLoad < maxIOLoadf, err },
		OnAquire:     func() { currentLoad += incrementBy },
		ConditionStr: fmt.Sprintf("current IO Load < %s", maxIOLoad),
	})

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

// GetIoWait returns the current IO wait, e.g. 0.0 for 0% and 100.0 for 100%
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

	total1 := Total(loadFirst[0])
	total2 := Total(loadSecond[0])

	iowait1 := loadFirst[0].Iowait
	iowait2 := loadSecond[0].Iowait

	// Calculate the average load
	averageLoad := (iowait2 - iowait1) / (total2 - total1) * 100

	return averageLoad
}

func Total(c cpu.TimesStat) float64 {
	total := c.User + c.System + c.Idle + c.Nice + c.Iowait + c.Irq +
		c.Softirq + c.Steal + c.Guest + c.GuestNice

	return total
}
