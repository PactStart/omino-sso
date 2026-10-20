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
	ClientName   string   `json:"clientName" binding:"required,min=1,max=128"`
	Description  string   `json:"description" binding:"omitempty,max=255"`
	LogoURL      string   `json:"logoUrl" binding:"omitempty,url,max=512"`
	RedirectURIs []string `json:"redirectUris" binding:"required,min=1,dive,url"`
	GrantTypes   []string `json:"grantTypes"`
	Scopes       []string `json:"scopes"`
	IsPublic     bool     `json:"isPublic"`
}

// CreateClient POST /api/v1/admin/clients
// clientSecret 仅此一次返回，不落日志。
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
	response.Created(c, gin.H{
		"clientId":     c0.ClientID,
		"clientSecret": secretPtr,
	})
}

// RotateSecretRequest POST /api/v1/admin/clients/:id/secret
type RotateSecretRequest struct {
	GracePeriodSeconds int `json:"gracePeriodSeconds"`
}

// RotateSecret 轮换应用密钥。
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
	response.Success(c, gin.H{
		"clientId":     clientID,
		"clientSecret": secret,
		"expiresAt":    nil,
	})
}
