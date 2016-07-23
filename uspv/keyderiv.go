package uspv

import (
	"fmt"
	"math/big"

	"github.com/roasbeef/btcd/btcec"
	"github.com/roasbeef/btcd/wire"
	"github.com/roasbeef/btcutil"
	"github.com/roasbeef/btcutil/hdkeychain"
)

/*
Key derivation for a TxStore has 3 levels: use case, peer index, and keyindex.
Regular wallet addresses are use 0, peer 0, and then a linear index.
The identity key is use 11, peer 0, index 0.
Channel multisig keys are use 2, peer and index per peer and channel.
Channel refund keys are use 3, peer and index per peer / channel.
*/

const (
	UseWallet          = 0
	UseChannelFund     = 2
	UseChannelRefund   = 3
	UseChannelHAKDBase = 4
	UseChannelElkrem   = 8
	UseChannelNonce    = 10 // links Id and channel. replaces UseChannelFund
	UseIdKey           = 11
)

// PrivKeyAddBytes adds bytes to a private key.
// NOTE that this modifies the key in place, overwriting it!!!!1
func PrivKeyAddBytes(k *btcec.PrivateKey, b []byte) {
	// turn arg bytes into a bigint
	arg := new(big.Int).SetBytes(b)
	// add private key to arg
	k.D.Add(k.D, arg)
	// mod 2^256ish
	k.D.Mod(k.D, btcec.S256().N)
	// new key derived from this sum
	// D is already modified, need to update the pubkey x and y
	k.X, k.Y = btcec.S256().ScalarBaseMult(k.D.Bytes())
	return
}

// PubKeyAddBytes adds bytes to a public key.
// NOTE that this modifies the key in place, overwriting it!!!!1
func PubKeyAddBytes(k *btcec.PublicKey, b []byte) {
	// turn b into a point on the curve
	bx, by := btcec.S256().ScalarBaseMult(b)
	// add arg point to pubkey point
	k.X, k.Y = btcec.S256().Add(bx, by, k.X, k.Y)
	return
}

// multiplies a pubkey point by a scalar
func PubKeyMultBytes(k *btcec.PublicKey, n uint32) {
	b := U32tB(n)
	k.X, k.Y = btcec.S256().ScalarMult(k.X, k.Y, b)
}

// multiply the private key by a coefficient
func PrivKeyMult(k *btcec.PrivateKey, n uint32) {
	bigN := new(big.Int).SetUint64(uint64(n))
	k.D.Mul(k.D, bigN)
	k.D.Mod(k.D, btcec.S256().N)
	k.X, k.Y = btcec.S256().ScalarBaseMult(k.D.Bytes())
}

// PubKeyArrAddBytes adds a byte slice to a serialized point.
// You can't add scalars to a point, so you turn the bytes into a point,
// then add that point.
func PubKeyArrAddBytes(p *[33]byte, b []byte) error {
	pub, err := btcec.ParsePubKey(p[:], btcec.S256())
	if err != nil {
		return err
	}
	// turn b into a point on the curve
	bx, by := pub.ScalarBaseMult(b)
	// add arg point to pubkey point
	pub.X, pub.Y = btcec.S256().Add(bx, by, pub.X, pub.Y)
	copy(p[:], pub.SerializeCompressed())
	return nil
}

// IDPointAdd adds an ID pubkey and a derivation point to make a channel pubkey.
func IDPointAdd(id, dp *[32]byte) ([33]byte, error) {

	cPub := new(btcec.PublicKey)
	cPub.Curve = btcec.S256()
	var cPubArr [33]byte

	// deserialize; IDs always start with 02
	idPub, err := IdToPub(*id)
	if err != nil {
		return cPubArr, err
	}
	// deserialize; derivation point starts with 03
	dPoint, err := btcec.ParsePubKey(append([]byte{0x03}, dp[:]...), btcec.S256())
	if err != nil {
		return cPubArr, err
	}

	// add the two points together
	cPub.X, cPub.Y = btcec.S256().Add(idPub.X, idPub.Y, dPoint.X, dPoint.Y)

	// return the new point, serialized
	copy(cPubArr[:], cPub.SerializeCompressed())
	return cPubArr, nil
}

// IDPrivAdd returns a channel pubkey from the sum of two scalars
func IDPrivAdd(idPriv, ds *btcec.PrivateKey) *btcec.PrivateKey {
	cPriv := new(btcec.PrivateKey)
	cPriv.Curve = btcec.S256()

	cPriv.D.Add(idPriv.D, ds.D)
	cPriv.D.Mod(cPriv.D, btcec.S256().N)

	cPriv.X, cPriv.Y = btcec.S256().ScalarBaseMult(cPriv.D.Bytes())
	return cPriv
}

func IdToPub(idArr [32]byte) (*btcec.PublicKey, error) {
	// IDs always start with 02
	return btcec.ParsePubKey(append([]byte{0x02}, idArr[:]...), btcec.S256())
}

// GetPrivkey generates and returns a private key derived from the seed.
// It will return nil if there's an error / problem, but there shouldn't be
// unless the root key itself isn't there or something.
// All other specialized derivation functions should call this.
func (t *TxStore) GetPrivkey(use, peerIdx, cIdx uint32) *btcec.PrivateKey {
	multiRoot, err := t.rootPrivKey.Child(use + hdkeychain.HardenedKeyStart)
	if err != nil {
		fmt.Printf("GetPrivkey err %s", err.Error())
		return nil
	}
	peerRoot, err := multiRoot.Child(peerIdx + hdkeychain.HardenedKeyStart)
	if err != nil {
		fmt.Printf("GetPrivkey err %s", err.Error())
		return nil
	}
	multiChild, err := peerRoot.Child(cIdx + hdkeychain.HardenedKeyStart)
	if err != nil {
		fmt.Printf("GetPrivkey err %s", err.Error())
		return nil
	}
	priv, err := multiChild.ECPrivKey()
	if err != nil {
		fmt.Printf("GetPrivkey err %s", err.Error())
		return nil
	}
	//	pubbyte := priv.PubKey().SerializeCompressed()
	//	fmt.Printf("- - -generated %d,%d,%d %x\n",
	//		use, peerIdx, cIdx, pubbyte[:8])
	return priv
}

// GetPubkey generates and returns the pubkey for a given path.
// It will return nil if there's an error / problem.
func (t *TxStore) GetPubkey(use, peerIdx, cIdx uint32) *btcec.PublicKey {
	priv := t.GetPrivkey(use, peerIdx, cIdx)
	if priv == nil {
		fmt.Printf("GetPubkey peer %d idx %d failed", peerIdx, cIdx)
		return nil
	}
	return priv.PubKey()
}

// GetAddress generates and returns the pubkeyhash address for a given path.
// It will return nil if there's an error / problem.
func (t *TxStore) GetAddress(
	use, peerIdx, cIdx uint32) *btcutil.AddressWitnessPubKeyHash {
	pub := t.GetPubkey(use, peerIdx, cIdx)
	if pub == nil {
		fmt.Printf("GetAddress %d,%d,%d made nil pub\n", use, peerIdx, cIdx)
		return nil
	}
	adr, err := btcutil.NewAddressWitnessPubKeyHash(
		btcutil.Hash160(pub.SerializeCompressed()), t.Param)
	if err != nil {
		fmt.Printf("GetAddress %d,%d,%d made nil pub\n", use, peerIdx, cIdx)
		return nil
	}
	return adr
}

// IdKey returns the identity private key, which is child(0).child(0) from root
func (t *TxStore) IdKey() *btcec.PrivateKey {
	return t.GetPrivkey(UseIdKey, 0, 0)
}

func (t *TxStore) IdPub() [33]byte {
	var b [33]byte
	k := t.GetPubkey(UseIdKey, 0, 0)
	if k != nil {
		copy(b[:], k.SerializeCompressed())
	}
	return b
}

// get a private key from the regular wallet
func (t *TxStore) GetWalletPrivkey(idx uint32) *btcec.PrivateKey {
	return t.GetPrivkey(UseWallet, 0, idx)
}

// get a public key from the regular wallet
func (t *TxStore) GetWalletAddress(idx uint32) *btcutil.AddressWitnessPubKeyHash {
	return t.GetAddress(UseWallet, 0, idx)
}

// ----- Get fund priv/pub replaced with channel / CKDH? -------------------

// GetFundPrivkey generates and returns the private key for a given peer, index.
// It will return nil if there's an error / problem, but there shouldn't be
// unless the root key itself isn't there or something.
func (t *TxStore) GetChanPrivkey(peerIdx, cIdx uint32) *btcec.PrivateKey {
	return t.GetPrivkey(UseChannelFund, peerIdx, cIdx)
}

// GetFundPubkey generates and returns the fund tx pubkey for a given index.
// It will return nil if there's an error / problem
func (t *TxStore) GetChanPubkey(peerIdx, cIdx uint32) [33]byte {
	var b [33]byte
	k := t.GetPubkey(UseChannelFund, peerIdx, cIdx)
	if k != nil {
		copy(b[:], k.SerializeCompressed())
	}
	return b
}

// ---------------------------------------------------------

// CreateChannelNonce returns the channel nonce used to get a CKDH.
// Maybe later this nonce can be the hash of some
// provable info, or a merkle root or something.
func (t *TxStore) CreateChanNonce(peerIdx, cIdx uint32) [20]byte {
	priv := t.GetPrivkey(UseChannelNonce, peerIdx, cIdx)
	var nonce [20]byte
	copy(nonce[:], btcutil.Hash160(priv.Serialize()))
	return nonce
}

// CalcCKDH calculates the channel key derivation hash from the two
// f is funder ID pub, r is receiver ID pub, cn is channel nonce.
func CalcCKDH(f, r [33]byte, cn [20]byte) wire.ShaHash {
	pre := make([]byte, 86)
	copy(pre[:33], f[:])
	copy(pre[33:66], r[:])
	copy(pre[66:], cn[:])
	return wire.DoubleSha256SH(pre)
}

// CalcChannelPub calculates the two channel pubkeys given a channel nonce.
// f is funder ID pub, r is receiver ID pub, cn is channel nonce.
func CalcChanPubs(f, r [33]byte, cn [20]byte) ([33]byte, [33]byte, error) {
	ckdh := CalcCKDH(f, r, cn)
	err := PubKeyArrAddBytes(&f, ckdh.Bytes())
	if err != nil {
		return f, r, err
	}
	err = PubKeyArrAddBytes(&r, ckdh.Bytes())
	return f, r, err
}

// GetFundAddress... like GetFundPubkey but hashes.  Useless/remove?
//func (ts *TxStore) GetFundAddress(
//	peerIdx, cIdx uint32) *btcutil.AddressWitnessPubKeyHash {
//	return ts.GetAddress(UseChannelFund, peerIdx, cIdx)
//}

// GetElkremRoot gives the Elkrem sender root hash for a channel.
func (t *TxStore) GetElkremRoot(peerIdx, cIdx uint32) wire.ShaHash {
	priv := t.GetPrivkey(UseChannelElkrem, peerIdx, cIdx)
	return wire.DoubleSha256SH(priv.Serialize())
}

// GetRefundPrivkey gives the private key for PKH refunds
func (t *TxStore) GetRefundPrivkey(peerIdx, cIdx uint32) *btcec.PrivateKey {
	return t.GetPrivkey(UseChannelRefund, peerIdx, cIdx)
}

// GetRefundPubkey gives the 33 byte serialized pubkey for PKH refunds
func (t *TxStore) GetRefundPubkey(peerIdx, cIdx uint32) [33]byte {
	var b [33]byte
	k := t.GetPubkey(UseChannelRefund, peerIdx, cIdx)
	if k != nil {
		copy(b[:], k.SerializeCompressed())
	}
	return b
}

// GetRefundPrivkey gives the private key for HAKD operations
func (t *TxStore) GetHAKDBasePriv(peerIdx, cIdx uint32) *btcec.PrivateKey {
	return t.GetPrivkey(UseChannelHAKDBase, peerIdx, cIdx)
}

// GetHAKDBasePoint gives the 33 byte serialized point for HAKD operations
func (t *TxStore) GetHAKDBasePoint(peerIdx, cIdx uint32) [33]byte {
	var b [33]byte
	k := t.GetPubkey(UseChannelHAKDBase, peerIdx, cIdx)
	if k != nil {
		copy(b[:], k.SerializeCompressed())
	}
	return b
}
