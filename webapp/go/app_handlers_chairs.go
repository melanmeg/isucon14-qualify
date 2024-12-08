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

// システムの混雑状況を判断するための構造体
type systemStatus struct {
	ActiveRides       int
	AvailableChairs   int
	PendingRidesCount int
}

// 適切な待ち時間を計算する関数
func calculateRetryAfterMs(ctx context.Context, tx *sqlx.Tx) (int, error) {
	var status systemStatus

	// アクティブな配車数を取得
	if err := tx.GetContext(ctx, &status.ActiveRides, `
		SELECT COUNT(DISTINCT r.id)
		FROM rides r
		JOIN ride_statuses rs ON r.id = rs.ride_id
		WHERE rs.status NOT IN ('COMPLETED', 'CANCELED')
		AND rs.created_at = (
			SELECT MAX(created_at)
			FROM ride_statuses
			WHERE ride_id = r.id
		)`); err != nil {
		return 0, err
	}

	// 利用可能な椅子の数を取得
	if err := tx.GetContext(ctx, &status.AvailableChairs, `
		SELECT COUNT(*)
		FROM chairs c
		WHERE c.is_active = TRUE
		AND NOT EXISTS (
			SELECT 1
			FROM rides r
			JOIN ride_statuses rs ON r.id = rs.ride_id
			WHERE r.chair_id = c.id
			AND rs.status NOT IN ('COMPLETED', 'CANCELED')
			AND rs.created_at = (
				SELECT MAX(created_at)
				FROM ride_statuses
				WHERE ride_id = r.id
			)
		)`); err != nil {
		return 0, err
	}

	// 待機中のライド数を取得
	if err := tx.GetContext(ctx, &status.PendingRidesCount, `
		SELECT COUNT(*)
		FROM rides r
		JOIN ride_statuses rs ON r.id = rs.ride_id
		WHERE rs.status = 'MATCHING'
		AND rs.created_at = (
			SELECT MAX(created_at)
			FROM ride_statuses
			WHERE ride_id = r.id
		)`); err != nil {
		return 0, err
	}

	// 基本の待ち時間は1000ms (1秒)
	baseRetryMs := 1000

	// システムの混雑状況に基づいて待ち時間を調整
	if status.AvailableChairs == 0 {
		// 利用可能な椅子がない場合は長めに待つ
		return baseRetryMs * 5, nil
	}

	// 待機中のライドと利用可能な椅子の比率に基づいて調整
	ratio := float64(status.PendingRidesCount) / float64(status.AvailableChairs)
	switch {
	case ratio > 2.0:
		// 非常に混雑している場合
		return baseRetryMs * 4, nil
	case ratio > 1.0:
		// やや混雑している場合
		return baseRetryMs * 3, nil
	case ratio > 0.5:
		// 通常の混雑状態
		return baseRetryMs * 2, nil
	default:
		// 空いている状態
		return baseRetryMs, nil
	}
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

	tx, err := db.Beginx()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	// retryAfterMs, err := calculateRetryAfterMs(ctx, tx)
	// if err != nil {
	// 	writeError(w, http.StatusInternalServerError, err)
	// 	return
	// }

	type nearbyChair struct {
		ID        string `db:"id"`
		Name      string `db:"name"`
		Model     string `db:"model"`
		Latitude  int    `db:"latitude"`
		Longitude int    `db:"longitude"`
	}

	query := `
		SELECT
			c.id,
			c.name,
			c.model,
			cl.latitude,
			cl.longitude
		FROM chairs c
		JOIN (
			SELECT DISTINCT ON (chair_id)
				chair_id,
				latitude,
				longitude,
				created_at
			FROM chair_locations
			ORDER BY chair_id, created_at DESC
		) cl ON c.id = cl.chair_id
		LEFT JOIN (
			SELECT DISTINCT ride_id, chair_id, status
			FROM ride_statuses rs
			JOIN rides r ON rs.ride_id = r.id
			WHERE rs.created_at = (
				SELECT MAX(created_at)
				FROM ride_statuses rs2
				WHERE rs2.ride_id = rs.ride_id
			)
		) current_status ON c.id = current_status.chair_id
		WHERE c.is_active = TRUE
		AND (current_status.status IS NULL OR current_status.status = 'COMPLETED')
		AND ABS(cl.latitude - ?) + ABS(cl.longitude - ?) <= ?
		ORDER BY ABS(cl.latitude - ?) + ABS(cl.longitude - ?)`

	chairs := []nearbyChair{}
	if err := tx.SelectContext(ctx, &chairs, query, lat, lon, distance, lat, lon); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if err := tx.Commit(); err != nil {
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
