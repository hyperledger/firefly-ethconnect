module github.com/hyperledger/firefly-ethconnect

require (
	github.com/Shopify/sarama v1.29.0
	github.com/alecthomas/template v0.0.0-20190718012654-fb15b899a751
	github.com/dsnet/compress v0.0.1 // indirect
	github.com/globalsign/mgo v0.0.0-20181015135952-eeefdecb41b8
	github.com/go-openapi/jsonreference v0.19.5
	github.com/go-openapi/spec v0.20.3
	github.com/go-openapi/swag v0.19.15 // indirect
	github.com/gorilla/websocket v1.4.2
	github.com/hashicorp/golang-lru v0.5.5-0.20210104140557-80c98217689d
	github.com/icza/dyno v0.0.0-20200205103839-49cb13720835
	github.com/julienschmidt/httprouter v1.3.0
	github.com/kaleido-io/ethbinding v0.0.0-20210527224458-a2622d9a2bde
	github.com/klauspost/compress v1.12.3 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-isatty v0.0.13 // indirect
	github.com/mgutz/ansi v0.0.0-20200706080929-d51e80ef957d // indirect
	github.com/mholt/archiver v3.1.1+incompatible
	github.com/nu7hatch/gouuid v0.0.0-20131221200532-179d4d0c4d8d
	github.com/nwaples/rardecode v1.1.0 // indirect
	github.com/oklog/ulid/v2 v2.0.2
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.1.3
	github.com/stretchr/testify v1.7.0
	github.com/syndtr/goleveldb v1.0.1-0.20210305035536-64b5b1c73954
	github.com/tidwall/gjson v1.8.0
	github.com/tidwall/pretty v1.1.1 // indirect
	github.com/ulikunitz/xz v0.5.10 // indirect
	github.com/x-cray/logrus-prefixed-formatter v0.5.2
	github.com/xi2/xz v0.0.0-20171230120015-48954b6210f8 // indirect
	golang.org/x/net v0.0.0-20210525063256-abc453219eb5 // indirect
	golang.org/x/term v0.0.0-20210503060354-a79de5458b56 // indirect
	gopkg.in/yaml.v2 v2.4.0
)

replace github.com/kaleido-io/ethbinding => ../ethbinding

go 1.16
