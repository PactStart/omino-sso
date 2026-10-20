package admin

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/store"
)

// ListAuditLogs GET /api/v1/admin/audit-logs
func (h *Handler) ListAuditLogs(c *gin.Context) {
	page, pageSize, err := pagination(c)
	if err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	f := store.AuditFilter{
		UserID:    c.Query("userId"),
		ClientID:  c.Query("clientId"),
		EventType: c.Query("eventType"),
		Result:    c.Query("result"),
		Page:      page,
		PageSize:  pageSize,
	}
	if v := c.Query("from"); v != "" {
		ms, err := parseTimeParam(v)
		if err != nil {
			response.Fail(c, response.ErrValidation.WithMessage("from 格式错误，需 RFC3339 或 Unix 毫秒"))
			return
		}
		f.From = ms
	}
	if v := c.Query("to"); v != "" {
		ms, err := parseTimeParam(v)
		if err != nil {
			response.Fail(c, response.ErrValidation.WithMessage("to 格式错误，需 RFC3339 或 Unix 毫秒"))
			return
		}
		f.To = ms
	}
	list, total, err := h.st.ListAuditEvents(c.Request.Context(), f)
	if err != nil {
		h.log.Error("list audit logs", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	out := make([]dto.AuditItem, 0, len(list))
	for i := range list {
		e := list[i]
		out = append(out, dto.AuditItem{
			ID:        e.ID,
			EventType: e.EventType,
			UserID:    e.UserID,
			Username:  e.Username,
			ClientID:  e.ClientID,
			IP:        e.IP,
			UserAgent: e.UserAgent,
			Result:    e.Result,
			Detail:    e.Detail,
			CreatedAt: dto.Time(e.CreatedAt),
		})
	}
	response.Success(c, response.Page{List: out, Total: total, Page: page, PageSize: pageSize})
}

func parseTimeParam(v string) (int64, error) {
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return 0, err
	}
	return t.UnixMilli(), nil
}
