package vulnverify

import "testing"

func TestUnverifiableMultiResponse(t *testing.T) {
	tpl := `
http:
  - raw:
      - "GET /a HTTP/1.1"
      - "GET /b HTTP/1.1"
    matchers:
      - type: dsl
        dsl: ["contains(body_2, \"ok\")"]
`
	if !UnverifiableTemplate(tpl) {
		t.Fatal("multi-response only template must be unverifiable")
	}
}

func TestVerifiableWhenSingleBlockExists(t *testing.T) {
	tpl := `
http:
  - matchers:
      - type: word
        words: ["Index of /"]
  - matchers:
      - type: dsl
        dsl: ["contains(body_2, \"ok\")"]
`
	if UnverifiableTemplate(tpl) {
		t.Fatal("template with a single-response block must stay loadable")
	}
}

func TestStatusOnlyIsVerifiable(t *testing.T) {
	tpl := `
http:
  - matchers:
      - type: status
        status: [200]
`
	if UnverifiableTemplate(tpl) {
		t.Fatal("status matcher is single-response")
	}
}
func TestTemplateWithoutMatchersIsNotBlocked(t *testing.T) {
	tpl := `
http:
  - method: GET
    path:
      - "{{BaseURL}}/"
    extractors:
      - type: regex
        part: body
        regex:
          - "v[0-9.]+"
`
	if UnverifiableTemplate(tpl) {
		t.Fatal("template without matchers must not be excluded")
	}
}
