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

	tx, err := db.Beginx()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	// まず全ての椅子を取得
	chairs := []Chair{}
	if err := tx.SelectContext(ctx, &chairs, "SELECT * FROM chairs WHERE owner_id = ?", owner.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	res := ownerGetSalesResponse{
		TotalSales: 0,
		Chairs:     make([]chairSales, len(chairs)),
		Models:     []modelSales{},
	}

	modelSalesMap := make(map[string]int)

	// 各椅子ごとに処理
	for i, chair := range chairs {
		// 完了したライドを取得
		rides := []Ride{}
		query := `
            SELECT r.* 
            FROM rides r
            JOIN (
                SELECT ride_id
                FROM ride_statuses
                WHERE status = 'COMPLETED'
                GROUP BY ride_id
            ) rs ON r.id = rs.ride_id
            WHERE r.chair_id = ?
            AND r.updated_at BETWEEN ? AND ?`

		if err := tx.SelectContext(ctx, &rides, query, chair.ID, since, until); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		// 売上計算
		sales := 0
		for _, ride := range rides {
			fare := calculateFare(
				ride.PickupLatitude,
				ride.PickupLongitude,
				ride.DestinationLatitude,
				ride.DestinationLongitude,
			)
			sales += fare
		}

		// 椅子ごとの売上を設定
		res.Chairs[i] = chairSales{
			ID:    chair.ID,
			Name:  chair.Name,
			Sales: sales,
		}

		// モデルごとの売上を集計
		modelSalesMap[chair.Model] += sales
		res.TotalSales += sales
	}

	// モデルごとの売上をレスポンスに追加
	for model, sales := range modelSalesMap {
		res.Models = append(res.Models, modelSales{
			Model: model,
			Sales: sales,
		})
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, res)
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
	// シンプルで確実なクエリに変更
	query := `
        SELECT 
            c.id,
            c.owner_id,
            c.name,
            c.access_token,
            c.model,
            c.is_active,
            c.created_at,
            c.updated_at,
            COALESCE(
                (SELECT SUM(
                    ABS(curr.latitude - prev.latitude) + 
                    ABS(curr.longitude - prev.longitude)
                )
                FROM chair_locations curr
                JOIN chair_locations prev 
                ON curr.chair_id = prev.chair_id
                AND prev.created_at = (
                    SELECT MAX(created_at) 
                    FROM chair_locations 
                    WHERE chair_id = curr.chair_id 
                    AND created_at < curr.created_at
                )
                WHERE curr.chair_id = c.id
                ), 0
            ) as total_distance,
            (SELECT MAX(created_at) 
             FROM chair_locations 
             WHERE chair_id = c.id
            ) as total_distance_updated_at
        FROM chairs c
        WHERE c.owner_id = ?
        ORDER BY c.created_at DESC`

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
