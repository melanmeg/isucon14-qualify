// webapp/go/internal_handlers.go
package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
)

// キャッシュを使わずに利用可能な椅子を取得
func getAvailableChairs() ([]Chair, error) {
	// 椅子のIDと利用可能かどうかを取得、また椅子のモデルからスピードを取得して結合する。
	rows, err := db.Query("SELECT c.id, cl.latitude, cl.longitude, cm.speed FROM chairs c JOIN chair_locations cl ON c.id = cl.chair_id JOIN chair_models cm ON c.model = cm.name WHERE c.is_active = TRUE ORDER BY cm.speed DESC;")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	availableChairs := []Chair{}
	for rows.Next() {
		var chair Chair
		if err := rows.Scan(&chair.ID, &chair.Speed, &chair.Latitude, &chair.Longitude); err != nil {
			slog.Debug(fmt.Sprintf("chair: %+v", chair))
			return nil, err
		}
		slog.Debug(fmt.Sprintf("chair: %+v", chair))
		availableChairs = append(availableChairs, chair)
	}
	slog.Debug(fmt.Sprintf("availableChairs: %+v", availableChairs))
	return availableChairs, nil
}

func pickBestChair(chairs []Chair, ride *Ride) Chair {
	bestScore := math.MinInt64
	bestChair := Chair{}

	for _, chair := range chairs {
		// 評価関数
		score := -abs(ride.PickupLatitude-chair.Latitude) - abs(ride.PickupLongitude-chair.Longitude)
		if score > bestScore {
			bestScore = score
			bestChair = chair
		}
	}

	return bestChair
}

func pickWorstChair(chairs []Chair, ride *Ride) Chair {
	worstScore := math.MinInt64
	worstChair := Chair{}

	for _, chair := range chairs {
		// 評価関数
		score := -abs(ride.PickupLatitude-chair.Latitude) - abs(ride.PickupLongitude-chair.Longitude)
		if score < worstScore {
			worstScore = score
			worstChair = chair
		}
	}

	return worstChair
}

// このAPIをインスタンス内から一定間隔で叩かせることで、椅子とライドをマッチングさせる
func internalGetMatching(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// MEMO: 一旦最も待たせているリクエストに適当な空いている椅子マッチさせる実装とする。おそらくもっといい方法があるはず…
	ride := &Ride{}
	if err := db.GetContext(ctx, ride, `SELECT * FROM rides WHERE chair_id IS NULL ORDER BY created_at LIMIT 1`); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	// "MATCHING"状態のライドの数を取得
	var matchingRideCount int
	if err := db.GetContext(ctx, &matchingRideCount, "SELECT COUNT(*) AS matching_count FROM (SELECT rs.ride_id FROM ride_statuses rs INNER JOIN (SELECT ride_id, MAX(created_at) AS latest_created_at FROM ride_statuses GROUP BY ride_id) AS sub ON rs.ride_id = sub.ride_id AND rs.created_at = sub.latest_created_at WHERE rs.status = 'MATCHING') AS latest_rides;"); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	chairs, err := getAvailableChairs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(chairs) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var chair Chair

	// 基本的に利用可能な椅子がライドよりもあまりにも少ない場合はカスみたいな椅子を提供する。
	if len(chairs)-matchingRideCount < 5 {
		chair = pickWorstChair(chairs, ride)
	} else {
		chair = pickBestChair(chairs, ride)
	}

	// データベース内でライドに椅子をアサイン
	tx, err := db.Beginx()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "UPDATE rides SET chair_id = ? WHERE id = ?", chair.ID, ride.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if _, err := tx.ExecContext(ctx, "UPDATE chairs SET is_active = FALSE WHERE id = ?", chair.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
