/internalGetMatching/ webapp/go/internal_handlers.go
package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
)

// 利用可能な椅子をキャッシュから取得
func getAvailableChairsFromCache(ctx context.Context) ([]Chair, error) {
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
func updateChairCache(ctx context.Context, chairID string, isAvailable bool) error {
	chairCache.mu.Lock()
	defer chairCache.mu.Unlock()

	if chair, found := chairCache.cache[chairID]; found {
		chair.IsActive = isAvailable
		chairCache.cache[chairID] = chair
		return nil
	}
	return errors.New("chair not found in cache")
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

	// 利用可能な椅子をキャッシュから取得
	chairs, err := getAvailableChairsFromCache(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if len(chairs) == 0 {
		// 椅子が見つからない場合にログを記録
		slog.Error("No available chairs for matching")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 最初の椅子を選択
	selectedChair := chairs[0]

	// ライドに椅子をアサイン
	if _, err := db.ExecContext(ctx, "UPDATE rides SET chair_id = ? WHERE id = ?", selectedChair.ID, ride.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	// キャッシュを更新
	if err := updateChairCache(ctx, selectedChair.ID, false); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	slog.Info("Matched chair to ride", "ride_id", ride.ID, "chair_id", selectedChair.ID)
	w.WriteHeader(http.StatusNoContent)
}
