package admin

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/api/response"
	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store/model"
)

// ListClients GET /api/v1/admin/clients
//
// @Summary      分页查询应用
// @Description  按关键字分页返回已注册的 OAuth 客户端，含 redirectUris / grantTypes / scopes / postLogoutUris；不返回任何密钥。
// @Tags         应用管理
// @Produce      json
// @Security     cookieAuth
// @Param        page      query  int     false  "页码，默认 1"
// @Param        pageSize  query  int     false  "每页条数，默认 20，上限 100"
// @Param        keyword   query  string  false  "应用标识或名称关键字"
// @Success      200       {object}  response.Body{data=response.Page{list=[]dto.Client,total=int64,page=int,pageSize=int}}  "success"
// @Failure      400       {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401       {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403       {object}  response.Body  "FORBIDDEN"
// @Failure      500       {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503       {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "admin"
// @Router       /admin/clients [get]
func (h *Handler) ListClients(c *gin.Context) {
	page, pageSize, err := pagination(c)
	if err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	list, total, err := h.client.List(c.Request.Context(), c.Query("keyword"), page, pageSize)
	if err != nil {
		h.log.Error("list clients", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	out := make([]dto.Client, 0, len(list))
	for i := range list {
		out = append(out, dto.FromClient(&list[i]))
	}
	response.Success(c, response.Page{List: out, Total: total, Page: page, PageSize: pageSize})
}

// CreateClientRequest 规范 §4.6。
type CreateClientRequest struct {
	ClientName   string   `json:"clientName" binding:"required,min=1,max=128" example:"统一门户"`
	Description  string   `json:"description" binding:"omitempty,max=255" example:"员工自助门户"`
	LogoURL      string   `json:"logoUrl" binding:"omitempty,url,max=512" example:"https://cdn.example.com/logo/portal.png"`
	RedirectURIs []string `json:"redirectUris" binding:"required,min=1,dive,url" example:"https://portal.example.com/callback"`
	GrantTypes   []string `json:"grantTypes" example:"authorization_code,refresh_token"`
	Scopes       []string `json:"scopes" example:"openid,profile,email"`
	IsPublic     bool     `json:"isPublic" example:"false"`
}

// CreateClient POST /api/v1/admin/clients
// clientSecret 仅此一次返回，不落日志。
//
// @Summary      注册应用
// @Description  注册 OAuth 客户端并返回生成的 clientId。**clientSecret 仅此一次返回**（不落日志、不可再查，丢失只能轮换）；公开客户端（isPublic=true）的 clientSecret 恒为 null。写入 client_created 审计事件。
// @Tags         应用管理
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        body  body  CreateClientRequest  true  "注册应用请求体"
// @Success      201   {object}  response.Body{data=dto.CreatedClient}  "注册成功"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      409   {object}  response.Body  "CLIENT_ALREADY_EXISTS"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "admin"
// @Router       /admin/clients [post]
func (h *Handler) CreateClient(c *gin.Context) {
	var req CreateClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrValidation)
		return
	}
	c0, secret, err := h.client.Create(c.Request.Context(), service.CreateClientInput{
		ClientName:   req.ClientName,
		Description:  req.Description,
		LogoURL:      req.LogoURL,
		RedirectURIs: req.RedirectURIs,
		GrantTypes:   req.GrantTypes,
		Scopes:       req.Scopes,
		IsPublic:     req.IsPublic,
	})
	if err != nil {
		h.log.Error("create client", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventClientCreated, ClientID: c0.ClientID,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
	})
	var secretPtr *string
	if secret != "" {
		secretPtr = &secret
	}
	response.Created(c, dto.CreatedClient{
		ClientID:     c0.ClientID,
		ClientSecret: secretPtr,
	})
}

// RotateSecretRequest POST /api/v1/admin/clients/:id/secret
type RotateSecretRequest struct {
	GracePeriodSeconds int `json:"gracePeriodSeconds"`
}

// RotateSecret 轮换应用密钥。
//
// @Summary      轮换应用密钥
// @Description  生成新的 clientSecret 并立即替换旧密钥（旧密钥随即失效，无宽限期；请求体的 gracePeriodSeconds 当前被忽略）。新密钥仅本次返回，expiresAt 恒为 null；写入 client_secret_rotated 审计事件。
// @Tags         应用管理
// @Accept       json
// @Produce      json
// @Security     cookieAuth
// @Param        id    path  string  true  "应用标识 clientId"
// @Param        body  body  RotateSecretRequest  false  "轮换参数（gracePeriodSeconds 暂未生效）"
// @Success      200   {object}  response.Body{data=dto.RotatedSecret}  "轮换成功"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED"
// @Failure      401   {object}  response.Body  "UNAUTHENTICATED"
// @Failure      403   {object}  response.Body  "FORBIDDEN"
// @Failure      404   {object}  response.Body  "NOT_FOUND"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Failure      503   {object}  response.Body  "DEPENDENCY_UNAVAILABLE"
// @x-auth-level  "admin"
// @Router       /admin/clients/{id}/secret [post]
func (h *Handler) RotateSecret(c *gin.Context) {
	var req RotateSecretRequest
	_ = c.ShouldBindJSON(&req)
	clientID := c.Param("id")
	secret, err := h.client.RotateSecret(c.Request.Context(), clientID)
	if err != nil {
		if errors.Is(err, service.ErrClientNotFound) {
			response.Fail(c, response.ErrNotFound)
			return
		}
		h.log.Error("rotate secret", "err", err)
		response.Fail(c, response.ErrInternal)
		return
	}
	h.audit.Emit(c.Request.Context(), service.Event{
		Type: model.EventClientSecretRotated, ClientID: clientID,
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: "success",
	})
	response.Success(c, dto.RotatedSecret{
		ClientID:     clientID,
		ClientSecret: secret,
		ExpiresAt:    nil,
	})
}
