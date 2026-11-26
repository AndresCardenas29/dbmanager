package pkcs12

// Añadido localmente por DB Manager: lista las entradas (bags) con sus atributos sin decodificar
// las llaves, de modo que funciona con cualquier tipo de llave (p. ej. Ed25519).

// BagInfo describe una entrada de un PKCS#12.
type BagInfo struct {
	Kind         string // "key" | "cert"
	FriendlyName string
	LocalKeyID   string // hexadecimal
}

// ListBags devuelve las entradas del PKCS#12 con friendlyName y localKeyId.
func ListBags(pfxData []byte, password string) ([]BagInfo, error) {
	encodedPassword, err := bmpStringZeroTerminated(password)
	if err != nil {
		return nil, ErrIncorrectPassword
	}
	bags, _, err := getSafeContents(pfxData, encodedPassword, 2, 2)
	if err != nil {
		return nil, err
	}
	var out []BagInfo
	for _, bag := range bags {
		bi := BagInfo{}
		switch {
		case bag.Id.Equal(oidCertBag):
			bi.Kind = "cert"
		case bag.Id.Equal(oidPKCS8ShroundedKeyBag), bag.Id.Equal(oidKeyBag):
			bi.Kind = "key"
		default:
			continue
		}
		for i := range bag.Attributes {
			k, v, err := convertAttribute(&bag.Attributes[i])
			if err != nil {
				continue
			}
			switch k {
			case "friendlyName":
				bi.FriendlyName = v
			case "localKeyId":
				bi.LocalKeyID = v
			}
		}
		out = append(out, bi)
	}
	return out, nil
}
