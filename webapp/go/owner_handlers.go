// webapp/go/owner_handlers.go
// webapp/go/owner_handlers.go
package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/oklog/ulid/v2"
)

const (
	initialFare     = 500
	farePerDistance = 100
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
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	owner := r.Context().Value("owner").(*Owner)

	// パラメータの解析
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

	// 椅子情報の取得
	chairs := []Chair{}
	if err := db.SelectContext(ctx, &chairs, "SELECT * FROM chairs WHERE owner_id = ?", owner.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	// 売上集計処理
	salesChan := make(chan int, len(chairs))
	errsChan := make(chan error, len(chairs))

	for _, chair := range chairs {
		go func(chair Chair) {
			rides := []Ride{}
			query := `
                SELECT r.* 
                FROM rides r
                JOIN ride_statuses rs ON r.id = rs.ride_id
                WHERE rs.status = 'COMPLETED'
                  AND r.chair_id = ?
                  AND r.updated_at BETWEEN ? AND ?`

			if err := db.SelectContext(ctx, &rides, query, chair.ID, since, until); err != nil {
				errsChan <- err
				return
			}

			sales := 0
			for _, ride := range rides {
				sales += calculateSale(ride)
			}
			salesChan <- sales
		}(chair)
	}

	totalSales := 0
	for i := 0; i < len(chairs); i++ {
		select {
		case sales := <-salesChan:
			totalSales += sales
		case err := <-errsChan:
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	// モデル別売上集計
	modelSalesMap := make(map[string]int)
	res := ownerGetSalesResponse{
		TotalSales: totalSales,
		Chairs:     []chairSales{},
		Models:     []modelSales{},
	}

	for _, chair := range chairs {
		sales := <-salesChan
		res.Chairs = append(res.Chairs, chairSales{
			ID:    chair.ID,
			Name:  chair.Name,
			Sales: sales,
		})
		modelSalesMap[chair.Model] += sales
	}

	for model, sales := range modelSalesMap {
		res.Models = append(res.Models, modelSales{
			Model: model,
			Sales: sales,
		})
	}

	writeJSON(w, http.StatusOK, res)
}
