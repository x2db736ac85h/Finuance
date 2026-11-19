package legacyimport

import (
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/investments"
)

// TestInvestmentRatesAreClamped: the withdrawal rate and the real return of the legacy document are
// clamped to their ranges, absurd values included (bounded before the int conversion, which CodeQL
// asked for), and an unreadable one falls back to the default.
func TestInvestmentRatesAreClamped(t *testing.T) {
	for _, c := range []struct {
		swr, ret         Num
		wantSwr, wantRet int
	}{
		{"4", "5", 400, 500},
		{"99999999999", "-99999999999", 1000, -1000},
		{"-99999999999", "99999999999", 50, 2000},
		{"abc", "", investments.DefaultSwrBp, investments.DefaultRealReturnBp},
	} {
		d, _ := investmentsOf(&Inv{Swr: c.swr, Ret: c.ret})
		if d.SwrBp != c.wantSwr || d.RealReturnBp != c.wantRet {
			t.Errorf("swr %q, ret %q: %d, %d; want %d, %d", c.swr, c.ret, d.SwrBp, d.RealReturnBp, c.wantSwr, c.wantRet)
		}
	}
}
