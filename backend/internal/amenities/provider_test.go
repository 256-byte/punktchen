package amenities

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/256-byte/punktchen/backend/gen/openapi"
	"github.com/256-byte/punktchen/backend/internal/layer"
)

// TestToiletsProvider: down before data; after an import the version is
// that import's id and stays the same until the data changes; payload and
// version come from the same state; degraded when not confirmed for 72 h.
func TestToiletsProvider(t *testing.T) {
	e := newImportEnv(t)
	ctx := context.Background()
	p := NewToiletsProvider(e.pool)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return now }

	if err := p.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if p.Version() != 0 || p.Health().Status != layer.StatusDown {
		t.Fatalf("before data: version %d, health %+v", p.Version(), p.Health())
	}
	if _, err := p.Nearby(ctx, layer.NearbyQuery{Center: layer.Location{Lat: 52.52, Lng: 13.41}, RadiusMeters: 500}); err == nil {
		t.Fatal("Nearby without data: want an error (layer down)")
	}

	recorded := fixture(t, "toiletten-20261118.json")
	e.wfs.set(http.StatusOK, recorded)
	first, err := e.im.Run(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	v1 := p.Version()
	if v1 != uint64(first.ImportID) {
		t.Fatalf("version %d, want import id %d", v1, first.ImportID)
	}

	// Alexanderplatz (cell centre near WC-Center Alexanderplatz 1).
	res, err := p.Nearby(ctx, layer.NearbyQuery{Center: layer.Location{Lat: 52.5215, Lng: 13.4113}, RadiusMeters: 590})
	if err != nil {
		t.Fatal(err)
	}
	payload := res.Payload.(*openapi.ToiletsLayer)
	if res.Version != v1 || payload.Layer != openapi.ToiletsLayerLayerToilets || len(payload.Features) != 4 {
		t.Fatalf("version %d, %d features", res.Version, len(payload.Features))
	}
	// Nearest: Bahnhof Alexanderplatz at 16 m (DATA_MODEL §7 #34).
	if f := payload.Features[0]; f.Address != "Bahnhof Alexanderplatz" || f.DistanceMeters != 16 {
		t.Errorf("nearest = %s at %d m", f.Address, f.DistanceMeters)
	}
	var wc *openapi.Toilet
	for i := range payload.Features {
		if payload.Features[i].Id == "toiletten.2" {
			wc = &payload.Features[i]
		}
	}
	if wc == nil || wc.Kind != openapi.ToiletKindToilet || wc.FeeEuro == nil || *wc.FeeEuro != 0.5 ||
		wc.Open24h || wc.OpeningHours == nil || *wc.OpeningHours != "Mo-Sa 07:00-22:00\nSo 10:00-22:00" {
		t.Errorf("WC-Center Alexanderplatz = %+v", wc)
	}

	// Unchanged answer: skipped, same version.
	if _, err := e.im.Run(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := p.Refresh(ctx); err != nil || p.Version() != v1 {
		t.Fatalf("after an unchanged run: version %d (want %d), %v", p.Version(), v1, err)
	}
	// Changed answer: new version.
	e.wfs.set(http.StatusOK, firstN(t, recorded, 500))
	second, err := e.im.Run(ctx, false)
	if err != nil || !second.Changed {
		t.Fatalf("%+v, %v", second, err)
	}
	if err := p.Refresh(ctx); err != nil || p.Version() != uint64(second.ImportID) || p.Version() == v1 {
		t.Fatalf("after a change: version %d, want %d", p.Version(), second.ImportID)
	}

	// Health follows the last confirmation (succeeded or skipped).
	p.now = time.Now
	if h := p.Health(); h.Status != layer.StatusOK {
		t.Fatalf("fresh data: %+v", h)
	}
	p.now = func() time.Time { return time.Now().Add(DefaultStaleAfter + time.Hour) }
	if h := p.Health(); h.Status != layer.StatusDegraded {
		t.Fatalf("73 h later: %+v", h)
	}
}

func TestFountainsProvider(t *testing.T) {
	e := newImportEnv(t)
	ctx := context.Background()
	im, err := NewImporter(e.pool, Fountains(e.wfsURL), Config{UserAgent: "punktchen-test", Scope: berlin}, e.im.il, e.im.log)
	if err != nil {
		t.Fatal(err)
	}
	e.wfs.set(http.StatusOK, fixture(t, "trinkwasserbrunnen-20261118.json"))
	run, err := im.Run(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	p := NewFountainsProvider(e.pool)
	if err := p.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	// Wedding: 3 fountains within 590 m (DATA_MODEL §7 #34).
	res, err := p.Nearby(ctx, layer.NearbyQuery{Center: layer.Location{Lat: 52.5427, Lng: 13.366}, RadiusMeters: 590})
	if err != nil {
		t.Fatal(err)
	}
	payload := res.Payload.(*openapi.FountainsLayer)
	if res.Version != uint64(run.ImportID) || len(payload.Features) < 3 {
		t.Fatalf("version %d, %d features", res.Version, len(payload.Features))
	}
	for i, f := range payload.Features {
		if f.Season != (openapi.Season{FromMonth: 5, ToMonth: 10}) || f.Place == "" {
			t.Errorf("feature %d = %+v", i, f)
		}
		if i > 0 && f.DistanceMeters < payload.Features[i-1].DistanceMeters {
			t.Errorf("not nearest first at %d", i)
		}
	}
	if info := p.Info(); info.ID != "fountains" || len(info.Attribution) != 1 {
		t.Errorf("info %+v", info)
	}
}
