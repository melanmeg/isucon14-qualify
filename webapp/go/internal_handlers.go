// webapp/go/internal_handlers.go
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// 利用可能な椅子をキャッシュから取得
func getAvailableChairsFromCache() ([]Chair, error) {
	chairCache.mu.RLock()
	defer chairCache.mu.RUnlock()

	availableChairs := []Chair{}
	for _, chair := range chairCache.cache {
		if chair.IsActive {
			availableChairs = append(availableChairs, chair)
		}
	}
	return availableChairs, nil
}

// キャッシュを更新
func updateChairCache(chairID string, isAvailable bool) {
	chairCache.mu.Lock()
	defer chairCache.mu.Unlock()

	if chair, exists := chairCache.cache[chairID]; exists {
		chair.IsActive = isAvailable
		chairCache.cache[chairID] = chair
	}
}

// キャッシュの再構築（主に初期化や大規模更新用）
func rebuildChairCache(ctx context.Context) error {
	rows, err := db.QueryContext(ctx, "SELECT id, is_active, owner_id, name, model, created_at, updated_at FROM chairs")
	if err != nil {
		return err
	}
	defer rows.Close()

	newCache := make(map[string]Chair)
	for rows.Next() {
		var chair Chair
		if err := rows.Scan(&chair.ID, &chair.IsActive, &chair.OwnerID, &chair.Name, &chair.Model, &chair.CreatedAt, &chair.UpdatedAt); err != nil {
			return err
		}
		newCache[chair.ID] = chair
	}

	chairCache.mu.Lock()
	defer chairCache.mu.Unlock()
	chairCache.cache = newCache
	return nil
}

// キャッシュを使わずに利用可能な椅子を取得
func getAvailableChairs() ([]Chair, error) {
	// 椅子のIDと利用可能かどうかを取得、また椅子のモデルからスピードを取得して結合する。
	rows, err := db.Query("SELECT c.id, cl.latitude, cl.longitude, cm.speed FROM chairs c JOIN chair_locations cl ON c.id = cl.chair_id JOIN chair_models cm ON c.model = cm.name WHERE c.is_active = TRUE ORDER BY cm.speed DESC;")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	slog.Debug(fmt.Sprintf("rows: %+v", rows))

	availableChairs := []Chair{}
	for rows.Next() {
		var chair Chair
		if err := rows.Scan(&chair.ID, &chair.Speed, &chair.Latituide, &chair.Longtitude); err != nil {
			slog.Debug(fmt.Sprintf("chair: %+v", chair))
			return nil, err
		}
		slog.Debug(fmt.Sprintf("chair: %+v", chair))
		availableChairs = append(availableChairs, chair)
	}
	slog.Debug(fmt.Sprintf("availableChairs: %+v", availableChairs))
	return availableChairs, nil
}

// 内部マッチング処理
func internalGetMatching(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 未マッチングのライドを取得
	ride := &Ride{}
	if err := db.GetContext(ctx, ride, `SELECT * FROM rides WHERE chair_id IS NULL ORDER BY created_at LIMIT 1`); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	// キャッシュから利用可能な椅子を取得
	availableChairs, err := getAvailableChairs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if len(availableChairs) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 最初の利用可能な椅子を選択
	selectedChair := availableChairs[0]

	// データベース内でライドに椅子をアサイン
	tx, err := db.Beginx()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, "UPDATE rides SET chair_id = ? WHERE id = ?", selectedChair.ID, ride.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	// キャッシュを更新
	updateChairCache(selectedChair.ID, false)

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
