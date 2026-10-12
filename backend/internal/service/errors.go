package service

import "errors"

// errNotImplemented 第一版骨架的统一未实现错误。
// handler 对返回该错误的调用统一回 501（api-contract skill：stub 允许 501，但路由必须注册）。
func errNotImplemented(what string) error {
	return &NotImplementedError{What: what}
}

type NotImplementedError struct{ What string }

func (e *NotImplementedError) Error() string {
	return "not implemented yet: " + e.What
}

// IsNotImplemented 判定 err 是否为骨架 stub 错误。
func IsNotImplemented(err error) bool {
	var nie *NotImplementedError
	return errors.As(err, &nie)
}

// ErrNotFound 资源不存在或非本人资源（属主校验：对外不区分，统一 404）。
var ErrNotFound = errors.New("not found")

// ErrValidation 参数/内容校验失败（对外 422 VALIDATION_FAILED）。
var ErrValidation = errors.New("validation failed")