// webapp/go/owner_handlers.go
package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/oklog/ulid/v2"
)

// トランザクション制御用のヘルパー関数
func withTx(ctx context.Context, db *sqlx.DB, fn func(*sqlx.Tx) error) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

const (
	initialFare     = 500
	farePerDistance = 100
)

type ownerPostOwnersRequest struct {
	Name string `json:"name"`
}

type ownerPostOwnersResponse struct {
	ID                 string `json:"id"`
	ChairRegisterToken string `json:"chair_register_token"`
}

func ownerPostOwners(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	req := &ownerPostOwnersRequest{}
	if err := bindJSON(r, req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, errors.New("some of required fields(name) are empty"))
		return
	}

	ownerID := ulid.Make().String()
	accessToken := secureRandomStr(32)
	chairRegisterToken := secureRandomStr(32)

	_, err := db.ExecContext(
		ctx,
		"INSERT INTO owners (id, name, access_token, chair_register_token) VALUES (?, ?, ?, ?)",
		ownerID, req.Name, accessToken, chairRegisterToken,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Path:  "/",
		Name:  "owner_session",
		Value: accessToken,
	})

	writeJSON(w, http.StatusCreated, &ownerPostOwnersResponse{
		ID:                 ownerID,
		ChairRegisterToken: chairRegisterToken,
	})
}

type chairSales struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Sales int    `json:"sales"`
}

type modelSales struct {
	Model string `json:"model"`
	Sales int    `json:"sales"`
}

type ownerGetSalesResponse struct {
	TotalSales int          `json:"total_sales"`
	Chairs     []chairSales `json:"chairs"`
	Models     []modelSales `json:"models"`
}

func ownerGetSales(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	owner := r.Context().Value("owner").(*Owner)

	// 期間のパース
	since := time.Unix(0, 0)
	until := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		parsed, err := strconv.ParseInt(sinceStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		since = time.UnixMilli(parsed)
	}
	if untilStr := r.URL.Query().Get("until"); untilStr != "" {
		parsed, err := strconv.ParseInt(untilStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		until = time.UnixMilli(parsed)
	}

	err := withTx(ctx, db, func(tx *sqlx.Tx) error {
		// 1回のクエリで必要なデータを全て取得
		type salesResult struct {
			ChairID    string `db:"chair_id"`
			ChairName  string `db:"chair_name"`
			ChairModel string `db:"chair_model"`
			PickupLat  int    `db:"pickup_lat"`
			PickupLon  int    `db:"pickup_lon"`
			DestLat    int    `db:"dest_lat"`
			DestLon    int    `db:"dest_lon"`
		}

		sales := []salesResult{}
		query := `
			SELECT 
				c.id as chair_id,
				c.name as chair_name,
				c.model as chair_model,
				r.pickup_latitude as pickup_lat,
				r.pickup_longitude as pickup_lon,
				r.destination_latitude as dest_lat,
				r.destination_longitude as dest_lon
			FROM chairs c
			LEFT JOIN rides r ON c.id = r.chair_id
			LEFT JOIN ride_statuses rs ON r.id = rs.ride_id
			WHERE c.owner_id = ?
			AND (rs.status = 'COMPLETED' OR rs.status IS NULL)
			AND (r.updated_at IS NULL OR (r.updated_at BETWEEN ? AND ? + INTERVAL 999 MICROSECOND))
			AND rs.created_at = (
				SELECT MAX(created_at)
				FROM ride_statuses
				WHERE ride_id = r.id
			)`

		if err := tx.SelectContext(ctx, &sales, query, owner.ID, since, until); err != nil {
			return err
		}

		// 集計処理
		chairSalesMap := make(map[string]*chairSales)
		modelSalesMap := make(map[string]int)
		totalSales := 0

		for _, s := range sales {
			if _, exists := chairSalesMap[s.ChairID]; !exists {
				chairSalesMap[s.ChairID] = &chairSales{
					ID:    s.ChairID,
					Name:  s.ChairName,
					Sales: 0,
				}
			}

			fare := calculateFare(s.PickupLat, s.PickupLon, s.DestLat, s.DestLon)
			chairSalesMap[s.ChairID].Sales += fare
			modelSalesMap[s.ChairModel] += fare
			totalSales += fare
		}

		// レスポンスの構築
		res := ownerGetSalesResponse{
			TotalSales: totalSales,
			Chairs:     make([]chairSales, 0, len(chairSalesMap)),
			Models:     make([]modelSales, 0, len(modelSalesMap)),
		}

		for _, cs := range chairSalesMap {
			res.Chairs = append(res.Chairs, *cs)
		}

		for model, sales := range modelSalesMap {
			res.Models = append(res.Models, modelSales{
				Model: model,
				Sales: sales,
			})
		}

		writeJSON(w, http.StatusOK, res)
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
}

type chairWithDetail struct {
	ID                     string       `db:"id"`
	OwnerID                string       `db:"owner_id"`
	Name                   string       `db:"name"`
	AccessToken            string       `db:"access_token"`
	Model                  string       `db:"model"`
	IsActive               bool         `db:"is_active"`
	CreatedAt              time.Time    `db:"created_at"`
	UpdatedAt              time.Time    `db:"updated_at"`
	TotalDistance          int          `db:"total_distance"`
	TotalDistanceUpdatedAt sql.NullTime `db:"total_distance_updated_at"`
}

type ownerGetChairResponse struct {
	Chairs []ownerGetChairResponseChair `json:"chairs"`
}

type ownerGetChairResponseChair struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	Model                  string `json:"model"`
	Active                 bool   `json:"active"`
	RegisteredAt           int64  `json:"registered_at"`
	TotalDistance          int    `json:"total_distance"`
	TotalDistanceUpdatedAt *int64 `json:"total_distance_updated_at,omitempty"`
}

func ownerGetChairs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	owner := ctx.Value("owner").(*Owner)

	chairs := []chairWithDetail{}
	// 移動距離計算を最適化したクエリ
	query := `
		WITH RECURSIVE chair_locations_with_prev AS (
			SELECT 
				cl1.chair_id,
				cl1.created_at,
				cl1.latitude,
				cl1.longitude,
				@prev_lat := NULL,
				@prev_lon := NULL,
				0 as distance
			FROM chair_locations cl1
			WHERE cl1.created_at = (
				SELECT MIN(created_at)
				FROM chair_locations cl2
				WHERE cl1.chair_id = cl2.chair_id
			)
			UNION ALL
			SELECT 
				cl.chair_id,
				cl.created_at,
				cl.latitude,
				cl.longitude,
				clp.latitude as prev_lat,
				clp.longitude as prev_lon,
				CASE 
					WHEN clp.latitude IS NULL THEN 0
					ELSE ABS(cl.latitude - clp.latitude) + ABS(cl.longitude - clp.longitude)
				END as distance
			FROM chair_locations cl
			JOIN chair_locations_with_prev clp ON cl.chair_id = clp.chair_id
			WHERE cl.created_at > clp.created_at
		)
		SELECT 
			c.id,
			c.owner_id,
			c.name,
			c.access_token,
			c.model,
			c.is_active,
			c.created_at,
			c.updated_at,
			COALESCE(d.total_distance, 0) as total_distance,
			d.last_update as total_distance_updated_at
		FROM chairs c
		LEFT JOIN (
			SELECT 
				chair_id,
				SUM(distance) as total_distance,
				MAX(created_at) as last_update
			FROM chair_locations_with_prev
			GROUP BY chair_id
		) d ON c.id = d.chair_id
		WHERE c.owner_id = ?`

	if err := db.SelectContext(ctx, &chairs, query, owner.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	res := ownerGetChairResponse{
		Chairs: make([]ownerGetChairResponseChair, len(chairs)),
	}

	for i, chair := range chairs {
		c := ownerGetChairResponseChair{
			ID:            chair.ID,
			Name:          chair.Name,
			Model:         chair.Model,
			Active:        chair.IsActive,
			RegisteredAt:  chair.CreatedAt.UnixMilli(),
			TotalDistance: chair.TotalDistance,
		}
		if chair.TotalDistanceUpdatedAt.Valid {
			t := chair.TotalDistanceUpdatedAt.Time.UnixMilli()
			c.TotalDistanceUpdatedAt = &t
		}
		res.Chairs[i] = c
	}

	writeJSON(w, http.StatusOK, res)
}
