package base58

import "testing"

func TestBase58_test2(t *testing.T) {

	testAddr := []string{
		"1QCaxc8hutpdZ62iKZsn1TCG3nh7uPZojq",
		"1DhRmSGnhPjUaVPAj48zgPV9e2oRhAQFUb",
		"17LN2oPYRYsXS9TdYdXCCDvF2FegshLDU2",
		"14h2bDLZSuvRFhUL45VjPHJcW667mmRAAn",
	}

	for ii, vv := range testAddr {
		// num := Base58Decode([]byte(vv))
		// chk := Base58Encode(num)
		num, err := FastBase58Decoding(vv)
		if err != nil {
			t.Errorf("Test %d, expected success, got error %s\n", ii, err)
		}
		chk := FastBase58Encoding(num)
		if vv != string(chk) {
			t.Errorf("Test %d, expected=%s got=%s Address did base58 encode/decode correctly.", ii, vv, chk)
		}
	}
}
