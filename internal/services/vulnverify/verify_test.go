package vulnverify

import "testing"

const yonyou = `
http:
  - method: GET
    matchers-condition: and
    matchers:
      - type: regex
        part: body
        regex:
          - '(?i)\[fonts\]'
      - type: status
        status: [200]
`

func TestYonyouFalsePositiveIsRejected(t *testing.T) {
	resp := "HTTP/1.1 200 OK\r\nServer: USAA\r\n\r\n<html>ok</html>"
	v := Check(yonyou, resp)
	if !v.Evaluable || v.Verified {
		t.Fatalf("want evaluable=false? got %+v", v)
	}
}

func TestYonyouRealEvidencePasses(t *testing.T) {
	resp := "HTTP/1.1 200 OK\r\n\r\n[fonts]\r\nfor 16-bit app support"
	v := Check(yonyou, resp)
	if !v.Evaluable || !v.Verified {
		t.Fatalf("want verified, got %+v", v)
	}
}

func TestStatusOnlyMatcherOn403IsRejected(t *testing.T) {
	tpl := `
http:
  - matchers:
      - type: status
        status: [200]
`
	resp := "HTTP/1.1 403 Forbidden\r\nServer: CloudFront\r\n\r\nAccess Denied"
	if v := Check(tpl, resp); !v.Evaluable || v.Verified {
		t.Fatalf("want rejected, got %+v", v)
	}
}

func TestDSLSingleResponseIsJudged(t *testing.T) {
	tpl := `
http:
  - matchers:
      - type: dsl
        dsl: ["status_code == 200 && contains(body, \"ok\")"]
`
	v := Check(tpl, "HTTP/1.1 200 OK\r\n\r\nok")
	if !v.Evaluable || !v.Verified || v.Weak {
		t.Fatalf("content dsl must be judged and strong, got %+v", v)
	}
	if v2 := Check(tpl, "HTTP/1.1 403 Forbidden\r\n\r\nno"); !v2.Evaluable || v2.Verified {
		t.Fatalf("want rejected, got %+v", v2)
	}

	statusOnly := `
http:
  - matchers:
      - type: dsl
        dsl: ["status_code == 200"]
`
	weak := Check(statusOnly, "HTTP/1.1 200 OK\r\n\r\nok")
	if !weak.Evaluable || !weak.Weak {
		t.Fatalf("status-only matcher must be weak, got %+v", weak)
	}
}

func TestNonHTTPResponseIsRejected(t *testing.T) {
	if v := Check(yonyou, "garbage"); !v.Evaluable || v.Verified {
		t.Fatalf("want rejected, got %+v", v)
	}
}
