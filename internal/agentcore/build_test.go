package agentcore

import "testing"

func TestLowestTCPPort(t *testing.T) {
	cases := []struct {
		name, in string
		want     int32
		wantErr  bool
	}{
		{name: "single tcp", in: `{"3000/tcp":{}}`, want: 3000},
		{name: "lowest of several tcp", in: `{"8080/tcp":{},"80/tcp":{},"443/tcp":{}}`, want: 80},
		{name: "skips udp", in: `{"53/udp":{},"8080/tcp":{}}`, want: 8080},
		{name: "no protocol defaults tcp", in: `{"3000":{}}`, want: 3000},
		{name: "only udp errors", in: `{"53/udp":{}}`, wantErr: true},
		{name: "null errors", in: `null`, wantErr: true},
		{name: "empty errors", in: ``, wantErr: true},
		{name: "garbage errors", in: `not json`, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := lowestTCPPort(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("lowestTCPPort(%q) = %d, want error", c.in, got)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("lowestTCPPort(%q) = %d, %v; want %d", c.in, got, err, c.want)
			}
		})
	}
}
