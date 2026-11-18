package fofa

import "errors"

var errInvalidFofaKey = errors.New("fofa key 长度不合法（应为 32 位）")
