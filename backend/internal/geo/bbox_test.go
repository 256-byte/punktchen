package geo

import "testing"

func TestParseBBox(t *testing.T) {
	berlin := BBox{MinLon: 13.088, MinLat: 52.338, MaxLon: 13.761, MaxLat: 52.675}

	tests := []struct {
		name    string
		in      string
		want    BBox
		wantErr bool
	}{
		{name: "berlin", in: "13.088,52.338,13.761,52.675", want: berlin},
		{name: "spaces allowed", in: " 13.088, 52.338 ,13.761 , 52.675", want: berlin},
		{name: "too few values", in: "13.088,52.338,13.761", wantErr: true},
		{name: "too many values", in: "1,2,3,4,5", wantErr: true},
		{name: "not a number", in: "13.088,abc,13.761,52.675", wantErr: true},
		{name: "min lon not below max lon", in: "13.761,52.338,13.088,52.675", wantErr: true},
		{name: "min lat not below max lat", in: "13.088,52.675,13.761,52.338", wantErr: true},
		{name: "lat and lon swapped (lat out of range)", in: "52.338,13.088,52.675,113.761", wantErr: true},
		{name: "lon out of range", in: "-181,52,13,53", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBBox(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseBBox(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseBBox(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ParseBBox(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestBBoxStringRoundTrip(t *testing.T) {
	const in = "13.088,52.338,13.761,52.675"
	b, err := ParseBBox(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != in {
		t.Fatalf("String() = %q, want %q", got, in)
	}
}
