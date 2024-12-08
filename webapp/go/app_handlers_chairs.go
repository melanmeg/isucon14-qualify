// webapp/go/app_handlers.go
package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
)

func getChairStats(ctx context.Context, tx *sqlx.Tx, chairID string) (appGetNotificationResponseChairStats, error) {
	stats := appGetNotificationResponseChairStats{}

	// 1回のクエリで必要なデータをすべて取得
	var result struct {
		TotalRides      int     `db:"total_rides"`
		TotalEvaluation float64 `db:"total_evaluation"`
	}

	err := tx.GetContext(
		ctx,
		&result,
		`WITH completed_rides AS (
			SELECT DISTINCT r.id, r.evaluation
			FROM rides r
			JOIN ride_statuses rs_completed ON r.id = rs_completed.ride_id
			JOIN ride_statuses rs_arrived ON r.id = rs_arrived.ride_id
			JOIN ride_statuses rs_carrying ON r.id = rs_carrying.ride_id
			WHERE r.chair_id = ?
			AND rs_completed.status = 'COMPLETED'
			AND rs_arrived.status = 'ARRIVED'
			AND rs_carrying.status = 'CARRYING'
			AND r.evaluation IS NOT NULL
		)
		SELECT
			COUNT(*) as total_rides,
			COALESCE(SUM(evaluation), 0) as total_evaluation
		FROM completed_rides`,
		chairID,
	)
	if err != nil {
		return stats, err
	}

	stats.TotalRidesCount = result.TotalRides
	if result.TotalRides > 0 {
		stats.TotalEvaluationAvg = result.TotalEvaluation / float64(result.TotalRides)
	}

	return stats, nil
}

type appGetNearbyChairsResponse struct {
	Chairs      []appGetNearbyChairsResponseChair `json:"chairs"`
	RetrievedAt int64                             `json:"retrieved_at"`
}

type appGetNearbyChairsResponseChair struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	Model             string     `json:"model"`
	CurrentCoordinate Coordinate `json:"current_coordinate"`
}

func appGetNearbyChairs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	latStr := r.URL.Query().Get("latitude")
	lonStr := r.URL.Query().Get("longitude")
	distanceStr := r.URL.Query().Get("distance")
	if latStr == "" || lonStr == "" {
		writeError(w, http.StatusBadRequest, errors.New("latitude or longitude is empty"))
		return
	}

	lat, err := strconv.Atoi(latStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("latitude is invalid"))
		return
	}

	lon, err := strconv.Atoi(lonStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("longitude is invalid"))
		return
	}

	distance := 50
	if distanceStr != "" {
		distance, err = strconv.Atoi(distanceStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, errors.New("distance is invalid"))
			return
		}
	}

	type nearbyChair struct {
		ID        string `db:"id"`
		Name      string `db:"name"`
		Model     string `db:"model"`
		Latitude  int    `db:"latitude"`
		Longitude int    `db:"longitude"`
	}

	// 1つのクエリですべての情報を取得
	query := `
        SELECT
            c.id,
            c.name,
            c.model,
            cl.latitude,
            cl.longitude
        FROM chairs c
        JOIN (
            SELECT chair_id, latitude, longitude
            FROM chair_locations cl1
            WHERE created_at = (
                SELECT MAX(created_at)
                FROM chair_locations cl2
                WHERE cl1.chair_id = cl2.chair_id
            )
        ) cl ON c.id = cl.chair_id
        WHERE c.is_active = TRUE
        AND NOT EXISTS (
            SELECT 1
            FROM rides r
            JOIN ride_statuses rs ON r.id = rs.ride_id
            WHERE r.chair_id = c.id
            AND rs.status != 'COMPLETED'
            AND rs.created_at = (
                SELECT MAX(created_at)
                FROM ride_statuses
                WHERE ride_id = r.id
            )
        )
        AND ABS(cl.latitude - ?) + ABS(cl.longitude - ?) <= ?
    `

	chairs := []nearbyChair{}
	if err := db.SelectContext(ctx, &chairs, query, lat, lon, distance); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	response := make([]appGetNearbyChairsResponseChair, len(chairs))
	for i, chair := range chairs {
		response[i] = appGetNearbyChairsResponseChair{
			ID:    chair.ID,
			Name:  chair.Name,
			Model: chair.Model,
			CurrentCoordinate: Coordinate{
				Latitude:  chair.Latitude,
				Longitude: chair.Longitude,
			},
		}
	}

	writeJSON(w, http.StatusOK, &appGetNearbyChairsResponse{
		Chairs:      response,
		RetrievedAt: time.Now().UnixMilli(),
	})
}
