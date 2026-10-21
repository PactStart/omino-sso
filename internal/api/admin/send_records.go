package admin

import (
	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service/notification"
)

// SendRecordItem 发送记录视图（target 已脱敏）。
type SendRecordItem struct {
	ID        string  `json:"id" example:"01J9ZK2Q3W5X7Y9A1B3C5D7E9F"`
	Channel   string  `json:"channel" example:"sms"`
	Scene     string  `json:"scene" example:"login"`
	Target    string  `json:"target" example:"138****8000"`
	Result    string  `json:"result" example:"success"`
	Provider  string  `json:"provider" example:"chuanglan"`
	Error     *string `json:"error" example:"null"`
	CreatedAt int64   `json:"createdAt" example:"1780000000000"`
}

// SendRecordPage 发送记录分页载荷。
type SendRecordPage struct {
	Items    []SendRecordItem `json:"items"`
	Total    int64            `json:"total" example:"42"`
	Page     int              `json:"page" example:"1"`
	PageSize int              `json:"pageSize" example:"20"`
}

// ListSendRecords GET /api/v1/admin/send-records
//
// @Summary      分页查询短信/邮箱发送记录
// @Description  按渠道、场景、结果与时间范围筛选验证码发送记录，created_at 倒序。未传 startTime/endTime 时默认最近 30 天（强制带时间范围以命中 created_at 索引）。target 为服务端脱敏后的接收方。
// @Tags         审计
// @Produce      json
// @Security     cookieAuth
// @Param        page       query  int     false  "页码，默认 1"
// @Param        pageSize   query  int     false  "每页条数，默认 20，上限 100"
// @Param        channel    query  string  false  "渠道"  Enums(sms, email)
// @Param        scene      query  string  false  "场景"  Enums(bind_phone, bind_email, login, find_password, mfa_unbind)
// @Param        result     query  string  false  "结果"  Enums(success, failed)
// @Param        startTime  query  string  false  "起始时间（Unix 毫秒或 RFC3339）"
// @Param        endTime    query  string  false  "结束时间（Unix 毫秒或 RFC3339）"
// @Success      200        {object}  response.Body{data=SendRecordPage}  "success"
// @Failure      400        {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401        {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403        {object}  response.Body  "FORBIDDEN"
// @Failure      500        {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503        {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "admin"
// @Router       /admin/send-records [get]
func (h *Handler) ListSendRecords(c *gin.Context) {
	page, pageSize, err := pagination(c)
	if err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	f := notification.RecordFilter{
		Channel:  c.Query("channel"),
		Scene:    c.Query("scene"),
		Result:   c.Query("result"),
		Page:     page,
		PageSize: pageSize,
	}
	if v := c.Query("startTime"); v != "" {
		if f.From, err = parseTimeParam(v); err != nil {
			response.Fail(c, response.ErrValidation.WithMessage("startTime 格式不正确"))
			return
		}
	}
	if v := c.Query("endTime"); v != "" {
		if f.To, err = parseTimeParam(v); err != nil {
			response.Fail(c, response.ErrValidation.WithMessage("endTime 格式不正确"))
			return
		}
	}
	records, total, err := h.notif.ListRecords(c.Request.Context(), f)
	if err != nil {
		h.log.Error("list send records", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	items := make([]SendRecordItem, 0, len(records))
	for _, r := range records {
		items = append(items, SendRecordItem{
			ID:        r.ID,
			Channel:   r.Channel,
			Scene:     r.Scene,
			Target:    r.Target,
			Result:    r.Result,
			Provider:  r.Provider,
			Error:     r.Error,
			CreatedAt: r.CreatedAt,
		})
	}
	response.Success(c, SendRecordPage{Items: items, Total: total, Page: page, PageSize: pageSize})
}
