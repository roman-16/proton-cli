module github.com/roman-16/proton-cli

go 1.26.5

require (
	github.com/ProtonMail/go-mime v0.0.0-20230322103455-7d82a3887f2f
	github.com/ProtonMail/go-srp v0.0.7
	github.com/ProtonMail/gopenpgp/v2 v2.10.0-proton
	github.com/fxamacker/cbor/v2 v2.9.2
	github.com/go-ctap/ctaphid v0.8.1
	github.com/goccy/go-yaml v1.19.2
	github.com/minio/selfupdate v0.6.0
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	github.com/teambition/rrule-go v1.8.2
	golang.org/x/mod v0.39.0
	golang.org/x/net v0.57.0
	golang.org/x/sys v0.47.0
	golang.org/x/term v0.45.0
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/ldclabs/cose v1.3.2 // indirect
	github.com/samber/lo v1.53.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
)

require (
	aead.dev/minisign v0.3.0 // indirect
	github.com/ProtonMail/bcrypt v0.0.0-20211005172633-e235017c1baf // indirect
	github.com/ProtonMail/go-crypto v1.4.1-proton // indirect
	github.com/cloudflare/circl v1.6.5 // indirect
	github.com/cronokirby/saferith v0.33.0 // indirect
	github.com/ebitengine/purego v0.10.2 // indirect
	github.com/go-ctap/winhello v0.1.0
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/telesma-app/ctap v0.49.0
	github.com/telesma-app/hid v0.12.0 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)

replace github.com/go-ctap/winhello => github.com/ProtonMail/winhello v0.0.0-20260223131736-d2c4f2d06287
