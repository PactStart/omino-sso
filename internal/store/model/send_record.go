package model

// SendRecord 对应 send_records 表（AGENTS.md §6，005-verification-code-auth）。
// 每次短信/邮箱验证码发送（成功或失败）写一条；target 为脱敏后的接收方，
// 不可反查真实联系方式；本表不使用软删除，按保留期物理清理。
type SendRecord struct {
	ID        string  `gorm:"column:id;primaryKey"`
	Channel   string  `gorm:"column:channel"`
	Scene     string  `gorm:"column:scene"`
	Target    string  `gorm:"column:target"`
	Result    string  `gorm:"column:result"`
	Provider  string  `gorm:"column:provider"`
	Error     *string `gorm:"column:error"`
	CreatedAt int64   `gorm:"column:created_at"`
}

func (SendRecord) TableName() string { return "send_records" }

// 渠道枚举。
const (
	SendChannelSMS   = "sms"
	SendChannelEmail = "email"
)

// 场景枚举（与 contracts/verification-api.md 一致）。
const (
	SendSceneBindPhone    string = "bind_phone"
	SendSceneBindEmail    string = "bind_email"
	SendSceneLogin        string = "login"
	SendSceneFindPassword string = "find_password"
	SendSceneMFAUnbind    string = "mfa_unbind"
)

// 结果枚举。
const (
	SendResultSuccess = "success"
	SendResultFailed  = "failed"
)

// 供应商枚举，取值即实际执行发送的实现名。
const (
	SendProviderChuanglan = "chuanglan"
	SendProviderSMTP      = "smtp"
	SendProviderMock      = "mock"
)
