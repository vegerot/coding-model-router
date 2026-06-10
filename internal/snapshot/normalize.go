package snapshot

// NormalizedQuality min–max normalizes each candidate's Quality over the given
// set, returning a map keyed by Slug with values in [0, 1].
//
// Normalization is deliberately set-dependent (and therefore NOT stored in the
// snapshot): the routing engine may select over a filtered subset, and the
// quality floor `p` must be relative to whatever set is in play.
//
// Edge cases: an empty set yields an empty map; a single candidate (or any set
// whose qualities are all equal) maps every member to 1.0, so a quality floor
// never excludes the only/best option.
//
// Cost needs no analogous helper: the V1 cost axis is a single measure
// (Candidate.BlendedPricePer1M), which the engine compares directly.
func NormalizedQuality(cands []Candidate) map[string]float64 {
	out := make(map[string]float64, len(cands))
	if len(cands) == 0 {
		return out
	}
	min, max := cands[0].Quality, cands[0].Quality
	for _, c := range cands[1:] {
		if c.Quality < min {
			min = c.Quality
		}
		if c.Quality > max {
			max = c.Quality
		}
	}
	span := max - min
	for _, c := range cands {
		if span == 0 {
			out[c.Slug] = 1.0
		} else {
			out[c.Slug] = (c.Quality - min) / span
		}
	}
	return out
}
