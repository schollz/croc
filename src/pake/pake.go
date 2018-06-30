package pake

import (
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math/big"

	"golang.org/x/crypto/bcrypt"
)

// Pake keeps public and private variables by
// only transmitting between parties after marshaling
//
// This method follows
// https://crypto.stanford.edu/~dabo/cryptobook/BonehShoup_0_4.pdf
// Figure 21/15
// http://www.lothar.com/~warner/MagicWormhole-PyCon2016.pdf
// Slide 11

type Pake struct {
	// Public variables
	Role     int
	Uᵤ, Uᵥ   *big.Int
	Vᵤ, Vᵥ   *big.Int
	Xᵤ, Xᵥ   *big.Int
	Yᵤ, Yᵥ   *big.Int
	HkA, HkB []byte

	// Private variables
	curve      elliptic.Curve
	pw         []byte
	vpwᵤ, vpwᵥ *big.Int
	upwᵤ, upwᵥ *big.Int
	α          []byte
	αᵤ, αᵥ     *big.Int
	zᵤ, zᵥ     *big.Int
	k          []byte

	isVerified bool
}

func Init(pw []byte, role int, curve elliptic.Curve) (p *Pake, err error) {
	p = new(Pake)
	if role == 1 {
		p.Role = 1
		p.curve = curve
		p.pw = pw
	} else {
		p.Role = 0
		p.curve = curve
		p.pw = pw
		rand1 := make([]byte, 8)
		rand2 := make([]byte, 8)
		rand.Read(rand1)
		rand.Read(rand2)
		p.Uᵤ, p.Uᵥ = p.curve.ScalarBaseMult(rand1)
		p.Vᵤ, p.Vᵥ = p.curve.ScalarBaseMult(rand2)
		if !p.curve.IsOnCurve(p.Uᵤ, p.Uᵥ) {
			err = errors.New("U values not on curve")
			return
		}
		if !p.curve.IsOnCurve(p.Vᵤ, p.Vᵥ) {
			err = errors.New("V values not on curve")
			return
		}

		// STEP: A computes X
		p.vpwᵤ, p.vpwᵥ = p.curve.ScalarMult(p.Vᵤ, p.Vᵥ, p.pw)
		p.upwᵤ, p.upwᵥ = p.curve.ScalarMult(p.Uᵤ, p.Uᵥ, p.pw)
		p.α = make([]byte, 8) // randomly generated secret
		rand.Read(p.α)
		p.αᵤ, p.αᵥ = p.curve.ScalarBaseMult(p.α)
		p.Xᵤ, p.Xᵥ = p.curve.Add(p.upwᵤ, p.upwᵥ, p.αᵤ, p.αᵥ) // "X"
		// now X should be sent to B
	}
	return
}

func (p *Pake) Bytes() []byte {
	b, _ := json.Marshal(p)
	return b
}

// Update will update itself
func (p *Pake) Update(qBytes []byte) (err error) {
	var q *Pake
	err = json.Unmarshal(qBytes, &q)
	if err != nil {
		return
	}
	if p.Role == q.Role {
		err = errors.New("can't have its own role")
		return
	}

	if p.Role == 1 {
		// initial step for B
		if p.Uᵤ == nil && q.Uᵤ != nil {
			// copy over public variables
			p.Uᵤ, p.Uᵥ = q.Uᵤ, q.Uᵥ
			p.Vᵤ, p.Vᵥ = q.Vᵤ, q.Vᵥ
			p.Xᵤ, p.Xᵥ = q.Xᵤ, q.Xᵥ

			// confirm that U,V are on curve
			if !p.curve.IsOnCurve(p.Uᵤ, p.Uᵥ) {
				err = errors.New("U values not on curve")
				return
			}
			if !p.curve.IsOnCurve(p.Vᵤ, p.Vᵥ) {
				err = errors.New("V values not on curve")
				return
			}

			// STEP: B computes Y
			p.vpwᵤ, p.vpwᵥ = p.curve.ScalarMult(p.Vᵤ, p.Vᵥ, p.pw)
			p.upwᵤ, p.upwᵥ = p.curve.ScalarMult(p.Uᵤ, p.Uᵥ, p.pw)
			p.α = make([]byte, 8) // randomly generated secret
			rand.Read(p.α)
			p.αᵤ, p.αᵥ = p.curve.ScalarBaseMult(p.α)
			p.Yᵤ, p.Yᵥ = p.curve.Add(p.vpwᵤ, p.vpwᵥ, p.αᵤ, p.αᵥ) // "Y"
			// STEP: B computes Z
			p.zᵤ, p.zᵥ = p.curve.Add(p.Xᵤ, p.Xᵥ, p.upwᵤ, new(big.Int).Neg(p.upwᵥ))
			p.zᵤ, p.zᵥ = p.curve.ScalarMult(p.zᵤ, p.zᵥ, p.α)
			// STEP: B computes k
			// H(pw,id_P,id_Q,X,Y,Z)
			HB := sha256.New()
			HB.Write(p.pw)
			HB.Write(p.Xᵤ.Bytes())
			HB.Write(p.Xᵥ.Bytes())
			HB.Write(p.Yᵤ.Bytes())
			HB.Write(p.Yᵥ.Bytes())
			HB.Write(p.zᵤ.Bytes())
			HB.Write(p.zᵥ.Bytes())
			// STEP: B computes k
			p.k = HB.Sum(nil)
			p.HkB, err = hashK(p.k)
		} else if p.HkA == nil && q.HkA != nil {
			p.HkA = q.HkA
			// verify
			err = checkKHash(p.HkA, p.k)
			if err == nil {
				p.isVerified = true
			}
		}
	} else {
		if p.HkB == nil && q.HkB != nil {
			p.HkB = q.HkB
			p.Yᵤ, p.Yᵥ = q.Yᵤ, q.Yᵥ

			// STEP: A computes Z
			p.zᵤ, p.zᵥ = p.curve.Add(p.Yᵤ, p.Yᵥ, p.vpwᵤ, new(big.Int).Neg(p.vpwᵥ))
			p.zᵤ, p.zᵥ = p.curve.ScalarMult(p.zᵤ, p.zᵥ, p.α)
			// STEP: A computes k
			// H(pw,id_P,id_Q,X,Y,Z)
			HA := sha256.New()
			HA.Write(p.pw)
			HA.Write(p.Xᵤ.Bytes())
			HA.Write(p.Xᵥ.Bytes())
			HA.Write(p.Yᵤ.Bytes())
			HA.Write(p.Yᵥ.Bytes())
			HA.Write(p.zᵤ.Bytes())
			HA.Write(p.zᵥ.Bytes())
			p.k = HA.Sum(nil)
			p.HkA, err = hashK(p.k)

			// STEP: A verifies that its session key matches B's
			// session key
			err = checkKHash(p.HkB, p.k)
			if err == nil {
				p.isVerified = true
			}
		}
	}
	return
}

// hashK generates a bcrypt hash of the password using work factor 14.
func hashK(k []byte) ([]byte, error) {
	return bcrypt.GenerateFromPassword(k, 14)
}

// checkKHash securely compares a bcrypt hashed password with its possible
// plaintext equivalent.  Returns nil on success, or an error on failure.
func checkKHash(hash, k []byte) error {
	return bcrypt.CompareHashAndPassword(hash, k)
}

// IsVerified returns whether or not the k has been
// generated AND it confirmed to be the same as partner
func (p *Pake) IsVerified() bool {
	return p.isVerified
}

// SessionKey is returned, unless it is not generated
// in which is returns an error. This function does
// not check if it is verifies.
func (p *Pake) SessionKey() ([]byte, error) {
	var err error
	if p.k == nil {
		err = errors.New("session key not generated")
	}
	return p.k, err
}

// func main() {
// 	// PUBLIC PARAMETERS (computed once)
// 	p256 := elliptic.P256()
// 	Uᵤ, Uᵥ := p256.ScalarBaseMult([]byte{1, 2, 3, 4})
// 	Vᵤ, Vᵥ := p256.ScalarBaseMult([]byte{1, 2, 3, 4})
// 	// PRIVATE PARAMATERS
// 	// pw
// 	pw := []byte{1, 1} // shared weak secret
// 	// PROTOCOL
// 	// STEP: A computes X
// 	upwᵤ, upwᵥ := p256.ScalarMult(Uᵤ, Uᵥ, pw)
// 	α := []byte{1, 2, 3, 4} // randomly generated secret
// 	αᵤ, αᵥ := p256.ScalarBaseMult(α)
// 	Xᵤ, Xᵥ := p256.Add(upwᵤ, upwᵥ, αᵤ, αᵥ) // "X"
// 	// STEP: A sends X
// 	// STEP: B computes Y
// 	vpwᵤ, vpwᵥ := p256.ScalarMult(Vᵤ, Vᵥ, pw)
// 	β := []byte{1, 2, 3, 4} // randomly generated secret
// 	gβᵤ, gβᵥ := p256.ScalarBaseMult(β)
// 	Yᵤ, Yᵥ := p256.Add(vpwᵤ, vpwᵥ, gβᵤ, gβᵥ) // "Y"
// 	// STEP: B computes Z
// 	BZᵤ, BZᵥ := p256.Add(Xᵤ, Xᵥ, upwᵤ, new(big.Int).Neg(upwᵥ))
// 	BZᵤ, BZᵥ = p256.ScalarMult(BZᵤ, BZᵥ, β)
// 	// STEP: B computes k
// 	// H(pw,id_P,id_Q,X,Y,Z)
// 	HB := sha256.New()
// 	HB.Write(pw)
// 	HB.Write(Xᵤ.Bytes())
// 	HB.Write(Xᵥ.Bytes())
// 	HB.Write(Yᵤ.Bytes())
// 	HB.Write(Yᵥ.Bytes())
// 	HB.Write(BZᵤ.Bytes())
// 	HB.Write(BZᵥ.Bytes())
// 	Bk := HB.Sum(nil)
// 	// STEP: B sends Y
// 	// STEP: A computes Z
// 	AZᵤ, AZᵥ := p256.Add(Yᵤ, Yᵥ, vpwᵤ, new(big.Int).Neg(vpwᵥ))
// 	AZᵤ, AZᵥ = p256.ScalarMult(AZᵤ, AZᵥ, α)
// 	// STEP: A computes k
// 	// H(pw,id_P,id_Q,X,Y,Z)
// 	HA := sha256.New()
// 	HA.Write(pw)
// 	HA.Write(Xᵤ.Bytes())
// 	HA.Write(Xᵥ.Bytes())
// 	HA.Write(Yᵤ.Bytes())
// 	HA.Write(Yᵥ.Bytes())
// 	HA.Write(AZᵤ.Bytes())
// 	HA.Write(AZᵥ.Bytes())
// 	Ak := HA.Sum(nil)
// 	// END
// 	// verify
// 	fmt.Println(Ak)
// 	fmt.Println(Bk)
// }
