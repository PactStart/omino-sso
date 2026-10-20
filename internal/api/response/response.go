package response

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/omino/omino-sso/internal/pkg/trace"
)

// Body 是 /api/v1/** 的统一响应结构（规范 §5）。
// data 字段永不省略；无内容时为 null。
type Body struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
	TraceID string `json:"traceId"`
}

// Page 是分页载荷。
type Page struct {
	List     any   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

// Success 返回成功响应。
func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{
		Code:    "OK",
		Message: "success",
		Data:    data,
		TraceID: trace.FromContext(c.Request.Context()),
	})
}

// Created 返回 201。
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Body{
		Code:    "OK",
		Message: "success",
		Data:    data,
		TraceID: trace.FromContext(c.Request.Context()),
	})
}

// Fail 按登记的错误码返回失败响应并中止后续处理。
func Fail(c *gin.Context, err *Error) {
	if err == nil {
		err = ErrInternal
	}
	c.AbortWithStatusJSON(err.HTTP, Body{
		Code:    err.Code,
		Message: err.Message,
		Data:    nil,
		TraceID: trace.FromContext(c.Request.Context()),
	})
}
