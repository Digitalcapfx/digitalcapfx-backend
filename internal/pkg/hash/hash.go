package hash

import "golang.org/x/crypto/bcrypt"

func PIN(pin string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPIN(hash, pin string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pin)) == nil
}
