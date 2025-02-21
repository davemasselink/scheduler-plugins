package peak

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"
	"sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler/config"
)

// Scheduler handles peak hour scheduling decisions
type Scheduler struct {
	config config.PeakHoursConfig
}

// New creates a new peak hours scheduler
func New(cfg config.PeakHoursConfig) *Scheduler {
	return &Scheduler{
		config: cfg,
	}
}

// IsPeakPeriod determines if the current time falls within a peak period
func (s *Scheduler) IsPeakPeriod(now time.Time) (bool, error) {
	if !s.config.Enabled || len(s.config.Schedules) == 0 {
		return false, nil
	}

	for _, schedule := range s.config.Schedules {
		if s.isTimeInSchedule(now, schedule) {
			return true, nil
		}
	}

	return false, nil
}

// GetCurrentThreshold returns the appropriate carbon intensity threshold
func (s *Scheduler) GetCurrentThreshold(baseThreshold float64, now time.Time) float64 {
	isPeak, err := s.IsPeakPeriod(now)
	if err != nil {
		klog.ErrorS(err, "Failed to check peak period, using base threshold")
		return baseThreshold
	}

	if isPeak {
		return s.config.CarbonIntensityThreshold
	}
	return baseThreshold
}

func (s *Scheduler) isTimeInSchedule(now time.Time, schedule config.Schedule) bool {
	// Check if current day is in schedule
	if !s.isDayInSchedule(now.Weekday(), schedule.DayOfWeek) {
		return false
	}

	// Parse schedule times
	startTime, err := time.Parse("15:04", schedule.StartTime)
	if err != nil {
		klog.ErrorS(err, "Invalid start time in schedule", "startTime", schedule.StartTime)
		return false
	}

	endTime, err := time.Parse("15:04", schedule.EndTime)
	if err != nil {
		klog.ErrorS(err, "Invalid end time in schedule", "endTime", schedule.EndTime)
		return false
	}

	// Compare only hours and minutes
	currentTime := time.Date(0, 1, 1, now.Hour(), now.Minute(), 0, 0, time.UTC)
	startTime = time.Date(0, 1, 1, startTime.Hour(), startTime.Minute(), 0, 0, time.UTC)
	endTime = time.Date(0, 1, 1, endTime.Hour(), endTime.Minute(), 0, 0, time.UTC)

	// Handle schedules that cross midnight
	if endTime.Before(startTime) {
		return currentTime.After(startTime) || currentTime.Before(endTime)
	}

	return currentTime.After(startTime) && currentTime.Before(endTime)
}

func (s *Scheduler) isDayInSchedule(current time.Weekday, scheduleDays string) bool {
	days := strings.Split(scheduleDays, ",")
	currentDay := int(current)

	for _, day := range days {
		dayRange := strings.Split(strings.TrimSpace(day), "-")
		if len(dayRange) == 1 {
			// Single day
			if d, err := strconv.Atoi(dayRange[0]); err == nil && d == currentDay {
				return true
			}
		} else if len(dayRange) == 2 {
			// Day range
			start, err1 := strconv.Atoi(dayRange[0])
			end, err2 := strconv.Atoi(dayRange[1])
			if err1 == nil && err2 == nil {
				// Handle ranges that cross week boundary
				if start > end {
					if currentDay >= start || currentDay <= end {
						return true
					}
				} else if currentDay >= start && currentDay <= end {
					return true
				}
			}
		}
	}

	return false
}

// GetNextPeakTransition returns the time until the next peak/off-peak transition
func (s *Scheduler) GetNextPeakTransition(now time.Time) (time.Time, bool, error) {
	if !s.config.Enabled || len(s.config.Schedules) == 0 {
		return time.Time{}, false, fmt.Errorf("peak scheduling not enabled")
	}

	var nextTransition time.Time
	currentlyPeak := false

	// Find the next transition time across all schedules
	for _, schedule := range s.config.Schedules {
		if transition, isPeak, err := s.getNextTransitionForSchedule(now, schedule); err == nil {
			if nextTransition.IsZero() || transition.Before(nextTransition) {
				nextTransition = transition
				currentlyPeak = isPeak
			}
		}
	}

	if nextTransition.IsZero() {
		return time.Time{}, false, fmt.Errorf("no valid transition found")
	}

	return nextTransition, currentlyPeak, nil
}

func (s *Scheduler) getNextTransitionForSchedule(now time.Time, schedule config.Schedule) (time.Time, bool, error) {
	startTime, err := time.Parse("15:04", schedule.StartTime)
	if err != nil {
		return time.Time{}, false, err
	}

	endTime, err := time.Parse("15:04", schedule.EndTime)
	if err != nil {
		return time.Time{}, false, err
	}

	// Create full timestamps for today's transitions
	today := now.Truncate(24 * time.Hour)
	todayStart := today.Add(time.Duration(startTime.Hour())*time.Hour + time.Duration(startTime.Minute())*time.Minute)
	todayEnd := today.Add(time.Duration(endTime.Hour())*time.Hour + time.Duration(endTime.Minute())*time.Minute)

	// Handle schedules that cross midnight
	if endTime.Before(startTime) {
		todayEnd = todayEnd.Add(24 * time.Hour)
	}

	// Find next valid day
	for i := 0; i < 7; i++ {
		checkDay := now.AddDate(0, 0, i)
		if s.isDayInSchedule(checkDay.Weekday(), schedule.DayOfWeek) {
			// Adjust transition times to this day
			daysToAdd := i
			nextStart := todayStart.AddDate(0, 0, daysToAdd)
			nextEnd := todayEnd.AddDate(0, 0, daysToAdd)

			if now.Before(nextStart) {
				return nextStart, true, nil
			}
			if now.Before(nextEnd) {
				return nextEnd, false, nil
			}
		}
	}

	return time.Time{}, false, fmt.Errorf("no transition found within next 7 days")
}
