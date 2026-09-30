package evm

import (
	"errors"
	"math/big"
)

// secp256k1 curve parameters.
var (
	secpP  = hexBig("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2F")
	secpN  = hexBig("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141")
	secpB  = big.NewInt(7)
	secpGx = hexBig("79BE667EF9DCBBAC55A06295CE870B07029BFCDB2DCE28D959F2815B16F81798")
	secpGy = hexBig("483ADA7726A3C4655DA4FBFC0E1108A8FD17B448A68554199C47D08FFB10D4B8")
)

type curvePoint struct {
	x, y *big.Int
}

func isOnCurve(p *curvePoint) bool {
	y2 := new(big.Int).Mul(p.y, p.y)
	y2.Mod(y2, secpP)
	x3 := new(big.Int).Mul(p.x, p.x)
	x3.Mul(x3, p.x)
	x3.Add(x3, secpB)
	x3.Mod(x3, secpP)
	return y2.Cmp(x3) == 0
}

func pointAdd(p1, p2 *curvePoint) *curvePoint {
	if p1 == nil {
		return p2
	}
	if p2 == nil {
		return p1
	}
	if p1.x.Cmp(p2.x) == 0 {
		if p1.y.Cmp(p2.y) != 0 || p1.y.Sign() == 0 {
			return nil
		}
		// Point doubling: m = 3x^2 / 2y (mod p).
		num := new(big.Int).Mul(p1.x, p1.x)
		num.Mul(num, big.NewInt(3))
		denom := new(big.Int).Lsh(p1.y, 1)
		denom.ModInverse(denom, secpP)
		num.Mul(num, denom)
		return completePointAdd(p1.x, p1.y, p2.x, num)
	}
	num := new(big.Int).Sub(p2.y, p1.y)
	denom := new(big.Int).Sub(p2.x, p1.x)
	denom.ModInverse(denom, secpP)
	num.Mul(num, denom)
	return completePointAdd(p1.x, p1.y, p2.x, num)
}

func completePointAdd(x1, y1, x2, slope *big.Int) *curvePoint {
	slope.Mod(slope, secpP)
	x3 := new(big.Int).Mul(slope, slope)
	x3.Sub(x3, x1)
	x3.Sub(x3, x2)
	x3.Mod(x3, secpP)
	y3 := new(big.Int).Sub(x1, x3)
	y3.Mul(y3, slope)
	y3.Sub(y3, y1)
	y3.Mod(y3, secpP)
	return &curvePoint{x: x3, y: y3}
}

func scalarMult(p *curvePoint, k *big.Int) *curvePoint {
	var result *curvePoint
	addend := &curvePoint{x: new(big.Int).Set(p.x), y: new(big.Int).Set(p.y)}
	for _, b := range k.Bytes() {
		for bit := 7; bit >= 0; bit-- {
			result = pointAdd(result, result)
			if b&(1<<uint(bit)) != 0 {
				result = pointAdd(result, addend)
			}
		}
	}
	return result
}

// recoverECDSA recovers the uncompressed 65-byte public key (0x04||X||Y)
// for an Ethereum-style signature (r, s, recoveryID in {0,1,2,3}).
func recoverECDSA(hash []byte, r, s *big.Int, recoveryID int) ([]byte, error) {
	if r.Sign() <= 0 || s.Sign() <= 0 || r.Cmp(secpN) >= 0 || s.Cmp(secpN) >= 0 {
		return nil, errors.New("invalid signature")
	}

	x := new(big.Int).Set(r)
	if recoveryID&2 != 0 {
		x.Add(x, secpN)
	}
	if x.Cmp(secpP) >= 0 {
		return nil, errors.New("invalid x coordinate")
	}

	// y = (x^3 + 7)^((p+1)/4) mod p, parity selected by the recovery id.
	alpha := new(big.Int).Exp(x, big.NewInt(3), secpP)
	alpha.Add(alpha, secpB)
	alpha.Mod(alpha, secpP)
	exp := new(big.Int).Add(secpP, big.NewInt(1))
	exp.Rsh(exp, 2)
	beta := new(big.Int).Exp(alpha, exp, secpP)
	if beta.Bit(0) != uint(recoveryID&1) {
		beta.Sub(secpP, beta)
	}

	R := &curvePoint{x: x, y: beta}
	if !isOnCurve(R) {
		return nil, errors.New("R not on curve")
	}

	e := new(big.Int).SetBytes(hash)
	e.Mod(e, secpN)

	rInv := new(big.Int).ModInverse(r, secpN)
	if rInv == nil {
		return nil, errors.New("no modular inverse")
	}

	sR := scalarMult(R, s)

	minusE := new(big.Int).Sub(secpN, e)
	minusE.Mod(minusE, secpN)
	G := &curvePoint{x: new(big.Int).Set(secpGx), y: new(big.Int).Set(secpGy)}
	eG := scalarMult(G, minusE)

	point := pointAdd(sR, eG)
	if point == nil || !isOnCurve(point) {
		return nil, errors.New("recovered point invalid")
	}
	Q := scalarMult(point, rInv)
	if Q == nil {
		return nil, errors.New("final multiplication failed")
	}

	out := make([]byte, 65)
	out[0] = 0x04
	xb := Q.x.Bytes()
	yb := Q.y.Bytes()
	copy(out[1+32-len(xb):33], xb)
	copy(out[33+32-len(yb):65], yb)
	return out, nil
}

func hexBig(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 16)
	if !ok {
		panic("invalid hex constant")
	}
	return v
}
