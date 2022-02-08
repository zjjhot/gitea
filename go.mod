module code.gitea.io/gitea

go 1.16

require (
	cloud.google.com/go v0.78.0 // indirect
	code.gitea.io/gitea-vet v0.2.2-0.20220122151748-48ebc902541b
	code.gitea.io/sdk/gitea v0.15.1
	gitea.com/go-chi/binding v0.0.0-20211013065440-d16dc407c2be
	gitea.com/go-chi/cache v0.0.0-20211013020926-78790b11abf1
	gitea.com/go-chi/captcha v0.0.0-20211013065431-70641c1a35d5
	gitea.com/go-chi/session v0.0.0-20211218221615-e3605d8b28b8
	gitea.com/lunny/levelqueue v0.4.1
	github.com/42wim/sshsig v0.0.0-20211121163825-841cf5bbc121
	github.com/Microsoft/go-winio v0.5.0 // indirect
	github.com/NYTimes/gziphandler v1.1.1
	github.com/ProtonMail/go-crypto v0.0.0-20210705153151-cc34b1f6908b // indirect
	github.com/PuerkitoBio/goquery v1.7.0
	github.com/alecthomas/chroma v0.10.0
	github.com/andybalholm/brotli v1.0.3 // indirect
	github.com/andybalholm/cascadia v1.2.0 // indirect
	github.com/blevesearch/bleve/v2 v2.3.0
	github.com/boombuler/barcode v1.0.1 // indirect
	github.com/bradfitz/gomemcache v0.0.0-20190913173617-a41fca850d0b // indirect
	github.com/caddyserver/certmagic v0.15.2
	github.com/chi-middleware/proxy v1.1.1
	github.com/couchbase/go-couchbase v0.0.0-20210224140812-5740cd35f448 // indirect
	github.com/couchbase/gomemcached v0.1.2 // indirect
	github.com/couchbase/goutils v0.0.0-20210118111533-e33d3ffb5401 // indirect
	github.com/denisenkom/go-mssqldb v0.10.0
	github.com/djherbis/buffer v1.2.0
	github.com/djherbis/nio/v3 v3.0.1
	github.com/duo-labs/webauthn v0.0.0-20220122034320-81aea484c951
	github.com/dustin/go-humanize v1.0.0
	github.com/editorconfig/editorconfig-core-go/v2 v2.4.2
	github.com/emirpasic/gods v1.12.0
	github.com/ethantkoenig/rupture v1.0.0
	github.com/gliderlabs/ssh v0.3.3
	github.com/go-asn1-ber/asn1-ber v1.5.3 // indirect
	github.com/go-chi/chi/v5 v5.0.4
	github.com/go-chi/cors v1.2.0
	github.com/go-enry/go-enry/v2 v2.7.1
	github.com/go-git/go-billy/v5 v5.3.1
	github.com/go-git/go-git/v5 v5.4.3-0.20210630082519-b4368b2a2ca4
	github.com/go-ldap/ldap/v3 v3.3.0
	github.com/go-redis/redis/v8 v8.11.0
	github.com/go-sql-driver/mysql v1.6.0
	github.com/go-swagger/go-swagger v0.27.0
	github.com/go-testfixtures/testfixtures/v3 v3.6.1
	github.com/gobwas/glob v0.2.3
	github.com/gogs/chardet v0.0.0-20191104214054-4b6791f73a28
	github.com/gogs/cron v0.0.0-20171120032916-9f6c956d3e14
	github.com/gogs/go-gogs-client v0.0.0-20210131175652-1d7215cd8d85
	github.com/golang-jwt/jwt/v4 v4.2.0
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/go-github/v39 v39.2.0
	github.com/google/uuid v1.3.0
	github.com/gorilla/feeds v1.1.1
	github.com/gorilla/mux v1.8.0 // indirect
	github.com/gorilla/sessions v1.2.1
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.0 // indirect
	github.com/hashicorp/go-version v1.3.1
	github.com/hashicorp/golang-lru v0.5.4
	github.com/huandu/xstrings v1.3.2
	github.com/jaytaylor/html2text v0.0.0-20200412013138-3577fbdbcff7
	github.com/json-iterator/go v1.1.12
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51
	github.com/kevinburke/ssh_config v1.1.0 // indirect
	github.com/keybase/go-crypto v0.0.0-20200123153347-de78d2cb44f4
	github.com/klauspost/compress v1.13.1
	github.com/klauspost/cpuid/v2 v2.0.9
	github.com/klauspost/pgzip v1.2.5 // indirect
	github.com/lib/pq v1.10.2
	github.com/lunny/dingtalk_webhook v0.0.0-20171025031554-e3534c89ef96
	github.com/markbates/goth v1.68.0
	github.com/mattn/go-isatty v0.0.13
	github.com/mattn/go-runewidth v0.0.13 // indirect
	github.com/mattn/go-sqlite3 v1.14.8
	github.com/mholt/archiver/v3 v3.5.0
	github.com/microcosm-cc/bluemonday v1.0.16
	github.com/minio/md5-simd v1.1.2 // indirect
	github.com/minio/minio-go/v7 v7.0.12
	github.com/minio/sha256-simd v1.0.0 // indirect
	github.com/mrjones/oauth v0.0.0-20190623134757-126b35219450 // indirect
	github.com/msteinert/pam v0.0.0-20201130170657-e61372126161
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646
	github.com/niklasfasching/go-org v1.5.0
	github.com/olekukonko/tablewriter v0.0.5 // indirect
	github.com/oliamb/cutter v0.2.2
	github.com/olivere/elastic/v7 v7.0.25
	github.com/pelletier/go-toml v1.9.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.8 // indirect
	github.com/pkg/errors v0.9.1
	github.com/pquerna/otp v1.3.0
	github.com/prometheus/client_golang v1.11.0
	github.com/quasoft/websspi v1.0.0
	github.com/rs/xid v1.3.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v5 v5.0.0
	github.com/sergi/go-diff v1.2.0
	github.com/shurcooL/httpfs v0.0.0-20190707220628-8d4bc4ba7749 // indirect
	github.com/shurcooL/vfsgen v0.0.0-20200824052919-0d455de96546
	github.com/ssor/bom v0.0.0-20170718123548-6386211fdfcf // indirect
	github.com/stretchr/testify v1.7.0
	github.com/syndtr/goleveldb v1.0.0
	github.com/tstranex/u2f v1.0.0
	github.com/ulikunitz/xz v0.5.10 // indirect
	github.com/unknwon/com v1.0.1
	github.com/unknwon/i18n v0.0.0-20210321134014-0ebbf2df1c44
	github.com/unknwon/paginater v0.0.0-20200328080006-042474bd0eae
	github.com/unrolled/render v1.4.0
	github.com/urfave/cli v1.22.5
	github.com/xanzy/go-gitlab v0.50.1
	github.com/yohcop/openid-go v1.0.0
	github.com/yuin/goldmark v1.4.4
	github.com/yuin/goldmark-highlighting v0.0.0-20210516132338-9216f9c5aa01
	github.com/yuin/goldmark-meta v1.0.0
	go.etcd.io/bbolt v1.3.6 // indirect
	go.jolheiser.com/hcaptcha v0.0.4
	go.jolheiser.com/pwn v0.0.3
	go.uber.org/atomic v1.9.0 // indirect
	go.uber.org/multierr v1.7.0 // indirect
	go.uber.org/zap v1.19.0 // indirect
	golang.org/x/crypto v0.0.0-20211117183948-ae814b36b871
	golang.org/x/net v0.0.0-20211112202133-69e39bad7dc2
	golang.org/x/oauth2 v0.0.0-20210628180205-a41e5a781914
	golang.org/x/sys v0.0.0-20211117180635-dee7805ff2e1
	golang.org/x/text v0.3.7
	golang.org/x/time v0.0.0-20210611083556-38a9dc6acbc6 // indirect
	golang.org/x/tools v0.1.0
	google.golang.org/protobuf v1.27.1 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
	gopkg.in/gomail.v2 v2.0.0-20160411212932-81ebce5c23df
	gopkg.in/ini.v1 v1.62.0
	gopkg.in/yaml.v2 v2.4.0
	mvdan.cc/xurls/v2 v2.2.0
	strk.kbt.io/projects/go/libravatar v0.0.0-20191008002943-06d1c002b251
	xorm.io/builder v0.3.9
	xorm.io/xorm v1.2.5
)

replace github.com/hashicorp/go-version => github.com/6543/go-version v1.3.1

replace github.com/markbates/goth v1.68.0 => github.com/zeripath/goth v1.68.1-0.20220109111530-754359885dce

replace github.com/shurcooL/vfsgen => github.com/lunny/vfsgen v0.0.0-20220105142115-2c99e1ffdfa0

replace github.com/satori/go.uuid v1.2.0 => github.com/gofrs/uuid v4.2.0+incompatible

exclude github.com/gofrs/uuid v3.2.0+incompatible

exclude github.com/gofrs/uuid v4.0.0+incompatible

exclude github.com/goccy/go-json v0.4.11
