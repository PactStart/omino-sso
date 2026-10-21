package admin

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
)

// ListAuditLogs GET /api/v1/admin/audit-logs
//
// @Summary      分页查询审计日志
// @Description  按用户、应用、事件类型、结果与时间范围筛选认证审计事件，按时间倒序分页返回；detail 为 JSON 字符串快照，无附加信息时为 null。
// @Tags         审计
// @Produce      json
// @Security     cookieAuth
// @Param        page       query  int     false  "页码，默认 1"
// @Param        pageSize   query  int     false  "每页条数，默认 20，上限 100"
// @Param        userId     query  string  false  "用户 ID"
// @Param        clientId   query  string  false  "应用标识"
// @Param        eventType  query  string  false  "事件类型，如 login_success / login_failure / logout"
// @Param        result     query  string  false  "结果"  Enums(success, failure)
// @Param        from       query  string  false  "起始时间，RFC3339 或 Unix 毫秒"
// @Param        to         query  string  false  "结束时间，RFC3339 或 Unix 毫秒"
// @Success      200        {object}  response.Body{data=response.Page{list=[]dto.AuditItem,total=int64,page=int,pageSize=int}}  "success"
// @Failure      400        {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401        {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403        {object}  response.Body  "FORBIDDEN"
// @Failure      500        {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503        {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "admin"
// @Router       /admin/audit-logs [get]
func (h *Handler) ListAuditLogs(c *gin.Context) {
	page, pageSize, err := pagination(c)
	if err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	f := service.AuditFilter{
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
	list, total, err := h.audit.List(c.Request.Context(), f)
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
