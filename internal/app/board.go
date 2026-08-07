package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/security"
	webpush "github.com/SherClockHolmes/webpush-go"
)

type boardRequest struct {
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	Priority    string `json:"priority"`
	Visibility  string `json:"visibility"`
	StartsOn    string `json:"startsOn"`
	EndsOn      string `json:"endsOn"`
	PushEnabled bool   `json:"pushEnabled"`
	Version     int64  `json:"version"`
}
type boardView struct {
	database.BoardItem
	Body       string `json:"body"`
	Summarized bool   `json:"summarized"`
}

func decodeBoardRequest(w http.ResponseWriter, r *http.Request) (boardRequest, bool) {
	var request boardRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_board_item", "게시 항목을 다시 확인해 주세요.")
		return request, false
	}
	return request, true
}
func boardFromRequest(id string, r boardRequest) database.BoardItem {
	return database.BoardItem{ID: id, Kind: r.Kind, Title: r.Title, Body: r.Body, Priority: r.Priority, Visibility: r.Visibility, StartsOn: r.StartsOn, EndsOn: r.EndsOn, PushEnabled: r.PushEnabled, Version: r.Version}
}
func (a *App) listBoardItems(w http.ResponseWriter, r *http.Request) {
	device, ok := a.authenticatedDevice(w, r)
	if !ok {
		return
	}
	location, _ := time.LoadLocation("Asia/Seoul")
	items, err := a.db.PublishedBoardItems(r.Context(), time.Now().In(location).Format("2006-01-02"))
	if err != nil {
		writeAPIError(w, 500, "board_list_failed", "게시 항목을 불러오지 못했습니다.")
		return
	}
	views := make([]boardView, 0, len(items))
	for _, item := range items {
		if item.Visibility == "parents_only" && (device.Type == "shared_tablet" || device.Type == "tv") {
			continue
		}
		view := boardView{BoardItem: item, Body: item.Body}
		if item.Visibility == "tv_summary" && device.Type == "tv" {
			view.Body = ""
			view.Summarized = true
		}
		views = append(views, view)
	}
	writeJSON(w, 200, map[string]any{"items": views})
}
func (a *App) createBoardItem(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireContentAdmin(w, r)
	if !ok {
		return
	}
	request, ok := decodeBoardRequest(w, r)
	if !ok {
		return
	}
	item, err := a.db.CreateBoardItem(r.Context(), boardFromRequest(security.NewToken(), request), device.ID, time.Now().UTC())
	if err != nil {
		writeAPIError(w, 400, "invalid_board_item", "게시 항목을 다시 확인해 주세요.")
		return
	}
	writeJSON(w, 201, item)
	if item.Kind == "notice" && item.Priority == "important" && item.PushEnabled {
		go a.sendBoardPush(item)
	}
}
func (a *App) updateBoardItem(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireContentAdmin(w, r)
	if !ok {
		return
	}
	request, ok := decodeBoardRequest(w, r)
	if !ok {
		return
	}
	item, err := a.db.UpdateBoardItem(r.Context(), boardFromRequest(r.PathValue("id"), request), device.ID, time.Now().UTC())
	if errors.Is(err, database.ErrBoardItemConflict) {
		writeAPIError(w, 409, "board_version_conflict", "다른 기기에서 게시 항목이 변경되었습니다.")
		return
	}
	if err != nil {
		writeAPIError(w, 400, "invalid_board_item", "게시 항목을 다시 확인해 주세요.")
		return
	}
	writeJSON(w, 200, item)
	if item.Kind == "notice" && item.Priority == "important" && item.PushEnabled {
		go a.sendBoardPush(item)
	}
}
func (a *App) setBoardStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		device, ok := a.requireContentAdmin(w, r)
		if !ok {
			return
		}
		var request struct {
			Version int64 `json:"version"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&request) != nil {
			writeAPIError(w, 400, "invalid_request", "요청을 다시 확인해 주세요.")
			return
		}
		err := a.db.SetBoardItemStatus(r.Context(), r.PathValue("id"), status, request.Version, device.ID, time.Now().UTC())
		if errors.Is(err, database.ErrBoardItemConflict) {
			writeAPIError(w, 409, "board_version_conflict", "다른 기기에서 게시 항목이 변경되었습니다.")
			return
		}
		if err != nil {
			writeAPIError(w, 404, "board_item_not_found", "게시 항목을 찾을 수 없습니다.")
			return
		}
		w.WriteHeader(204)
	}
}
func (a *App) listBoardAdmin(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.requireContentAdminRead(w, r); !ok {
			return
		}
		var items []database.BoardItem
		var err error
		if status == "archived" {
			items, err = a.db.ArchivedBoardItems(r.Context())
		} else {
			items, err = a.db.TrashedBoardItems(r.Context())
		}
		if err != nil {
			writeAPIError(w, 500, "board_list_failed", "게시 항목을 불러오지 못했습니다.")
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	}
}
func (a *App) permanentlyDeleteBoardItem(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireContentAdmin(w, r); !ok {
		return
	}
	var request struct {
		Version int64 `json:"version"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&request) != nil {
		writeAPIError(w, 400, "invalid_request", "요청을 다시 확인해 주세요.")
		return
	}
	if err := a.db.PermanentlyDeleteBoardItem(r.Context(), r.PathValue("id"), request.Version); err != nil {
		writeAPIError(w, 404, "board_item_not_found", "휴지통 항목을 찾을 수 없습니다.")
		return
	}
	w.WriteHeader(204)
}

func (a *App) sendBoardPush(item database.BoardItem) {
	if a.config.VAPIDPublicKey == "" {
		return
	}
	ctx := context.Background()
	subscriptions, err := a.db.ActiveParentPushSubscriptions(ctx)
	if err != nil {
		a.logger.Error("board push subscriptions failed", "error", err)
		return
	}
	for _, subscription := range subscriptions {
		claimed, err := a.db.ClaimBoardPush(ctx, item.ID, subscription.DeviceID, time.Now().UTC())
		if err != nil || !claimed {
			continue
		}
		body := item.Title
		if item.Visibility == "parents_only" {
			body = "새로운 중요 공지가 있습니다"
		}
		payload, _ := json.Marshal(map[string]string{"title": "중요 공지", "body": body, "path": "/", "tag": "board-" + item.ID})
		response, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{Endpoint: subscription.Endpoint, Keys: webpush.Keys{P256dh: subscription.P256DH, Auth: subscription.Auth}}, &webpush.Options{HTTPClient: a.pushClient, Subscriber: "https://family-dashboard.local", TTL: 3600, Topic: fmt.Sprintf("board-%.20s", item.ID), VAPIDPublicKey: a.config.VAPIDPublicKey, VAPIDPrivateKey: a.config.VAPIDPrivateKey})
		if err != nil {
			_ = a.db.ReleaseBoardPush(ctx, item.ID, subscription.DeviceID)
			continue
		}
		response.Body.Close()
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			_ = a.db.MarkBoardPushSent(ctx, item.ID, subscription.DeviceID, time.Now().UTC())
		} else {
			_ = a.db.ReleaseBoardPush(ctx, item.ID, subscription.DeviceID)
		}
	}
}
