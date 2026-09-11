package runner

import (
	"testing"

	"github.com/redbotster/nanobots/internal/schema"
)

func TestNeedsRealPDFRender(t *testing.T) {
	cases := []struct {
		name  string
		steps []schema.Step
		want  bool
	}{
		{"no render step", []schema.Step{{Type: "service.call"}}, false},
		{"render to html", []schema.Step{{Type: "transform.render", To: "html"}}, false},
		{"render to pdf", []schema.Step{{Type: "transform.render", To: "pdf"}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nb := &schema.Nanobot{Spec: schema.NanobotSpec{Steps: c.steps}}
			if got := needsRealPDFRender(nb); got != c.want {
				t.Errorf("needsRealPDFRender = %v, want %v", got, c.want)
			}
		})
	}
}
