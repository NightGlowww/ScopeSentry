package audit

import "errors"

var errTooLarge = errors.New("pageSize 不能超过 200")
