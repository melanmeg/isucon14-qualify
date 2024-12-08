package main

import (
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
)

func isValidStatusTransition(currentStatus, newStatus string) bool {
	validTransitions := map[string][]string{
		"MATCHING": {"ENROUTE"},
		"ENROUTE":  {"PICKUP"},
		"PICKUP":   {"CARRYING"},
		"CARRYING": {"ARRIVED"},
		"ARRIVED":  {"COMPLETED"},
	}
	nextStatuses, exists := validTransitions[currentStatus]
	if !exists {
		return false
	}
	for _, status := range nextStatuses {
		if status == newStatus {
			return true
		}
	}
	return false
}

func updateRideStatus(rideID, newStatus string) error {
	currentStatus := ""
	err := db.Get(&currentStatus, `SELECT status FROM ride_statuses WHERE ride_id = ? ORDER BY created_at DESC LIMIT 1`, rideID)
	if err != nil {
		return err
	}

	if !isValidStatusTransition(currentStatus, newStatus) {
		return errors.New("invalid status transition")
	}

	_, err = db.Exec(`INSERT INTO ride_statuses (id, ride_id, status) VALUES (?, ?, ?)`, generateID(), rideID, newStatus)
	return err
}

func generateID() string {
	t := time.Now()
	entropy := ulid.Monotonic(nil, 0)
	return ulid.MustNew(ulid.Timestamp(t), entropy).String()
}
