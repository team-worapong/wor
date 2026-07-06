# คำสั่งทั้งหมดของ wor

เอกสารนี้สรุปคำสั่ง CLI ทั้งหมดของ `wor` ตามที่ implement จริงใน
`internal/cliapp` (ดู `internal/cliapp/usage.go` เป็นแหล่งอ้างอิงหลักที่
sync กับโค้ดเสมอ -- ถ้าเอกสารนี้กับ `usage.go` ขัดแย้งกัน ให้ยึด
`usage.go`/พฤติกรรมจริงของโค้ดเป็นหลัก)

## ทั่วไป

- `wor version` / `wor --version` -- แสดงชื่อโปรแกรมและเวอร์ชัน
- `wor setup` -- wizard ตั้งค่าระบบครั้งแรก (environment, WOR_HOME, host
  provider, SSL provider, php-fpm endpoint) รันซ้ำได้ทุกเมื่อ ค่าที่เคย
  ตั้งไว้แล้วจะถูกใช้เป็นค่า default แทนการรีเซ็ตทับ
  จบ wizard ทุกครั้งจะตรวจ/รีเฟรช default vhost (`000_wor_default.conf`)
  ด้วย: ถ้าไฟล์เดิม (เช่นตกค้างจาก installation เก่าที่ใช้ WOR_HOME อื่น)
  ชี้ document root ไม่ตรงกับ workspace ปัจจุบัน จะ regenerate ให้ใหม่
  พร้อมเตือน -- เช็คแบบเทียบเนื้อไฟล์ (stateless) ไม่ใช่แค่ "WOR_HOME
  เปลี่ยนตอน setup" ดังนั้น `wor doctor`/`wor host add`/`wor reset`
  ที่เรียก EnsureDefaultHost เหมือนกันก็ได้พฤติกรรมซ่อมตัวเองนี้ด้วย
  (ไฟล์ที่ยังชี้ path ถูกต้องจะไม่ถูกแตะ การแก้ไขส่วนอื่นของ admin คงอยู่)
- `wor doctor` -- ตรวจสุขภาพระบบแบบ read-only แสดง checklist
  ✓/⚠/✗ ของ environment, runtime (Node.js/Go/Python/PHP/PHP-FPM),
  web server ที่ active, ฐานข้อมูล (optional เสมอ), และเครื่องมืออื่น ๆ
  (git/zip/gzip, optional เสมอ) exit code ไม่เป็นศูนย์เมื่อมีอะไรที่
  "จำเป็น" ขาดหายไป (runtime หลัก, host provider ที่ตั้งค่าไว้แต่ไม่ได้
  ติดตั้ง, หรือ workspace ยังไม่ initialize)
  ท้ายสุดมี section "Security" (⚠ เท่านั้น ไม่ทำให้ exit code ไม่เป็นศูนย์):
  (1) สแกนหาไฟล์ `.env`/`.env.*` ใต้ WOR_HOME ที่เปิด permission ให้
  group/other อ่านได้ พร้อม list path และคำสั่ง `find ... -exec chmod
  600` ให้ก็อปวางแก้ (2) เช็คว่า WOR_HOME (และ path บรรพบุรุษทั้งหมด)
  กับ `domains/<domain>/<service>/public` ของทุก static/php service
  ที่ลงทะเบียนไว้ web server user (เดา/อ่านจาก `user` directive ใน
  nginx.conf, default `www-data`) traverse เข้าไปได้จริงไหม -- เจอเต็ม
  รูปแบบเฉพาะ Debian/Ubuntu (แนะนำคำสั่ง `setfacl` ให้ตรงตัว), Linux
  ตระกูลอื่น (RHEL ฯลฯ) แค่เตือนกว้าง ๆ ให้เช็ค SELinux เพิ่มด้วย,
  ข้ามการเช็คนี้บน macOS/Windows (macOS: nginx ผ่าน Homebrew มักรันเป็น
  login user เอง ไม่ใช่ system account แยก; Windows: คนละ permission
  model ไปเลย)
- `wor env` -- แสดงค่า config/environment ปัจจุบัน
- `wor clean` -- ลบ host config/PM2 process/systemd unit/`/etc/hosts`
  entry ที่กลายเป็น orphan (ไม่มี domain/service อ้างอิงอยู่แล้ว) แต่ไม่แตะ
  อะไรที่ยังผูกกับ service ที่ registered อยู่
- `wor reset` -- ล้างทุกอย่างที่ wor สร้างไว้กลับสู่สภาพเริ่มต้น
  (PM2 process ที่ขึ้นต้นด้วย `wor_`, systemd unit `wor_*.service`, host
  config `wor__*.conf`, entry ใน `/etc/hosts`, โฟลเดอร์ domains/backups/
  logs/ssl) **ไม่ลบ** host config อื่นที่ไม่ใช่ของ wor ต้องพิมพ์ `RESET`
  ยืนยันเสมอ ไม่มี flag ข้ามการยืนยันนี้ได้

## wor create

```
wor create [host]
```

แบบ interactive-only ล้วน ๆ ไม่รับ flag อื่นใดนอกจาก host ที่เป็น
positional argument (ไม่บังคับ) จะถาม service type, การ override
domain id, domain type (local/public), และการตั้งค่า hosts entry ทีละ
ขั้นตอน งานที่ต้อง automate ให้ใช้ `wor domain/service/host add` แทน

## Domain

```
wor domain add <domain-id>
wor domain remove <domain-id>
```

`domain add` สร้างโฟลเดอร์/ไฟล์ config พื้นฐานของ domain (รวมถึงโฟลเดอร์
backup/log ที่เกี่ยวข้อง) `domain remove` **บล็อกทันที** ถ้า domain
ยังมี service ลงทะเบียนอยู่ (แม้จะหยุดทำงานอยู่ก็ตาม) ต้อง
`wor service remove` แต่ละตัวออกก่อน เพราะ "domain" ในความหมายของ wor
คือแค่โฟลเดอร์ config/source เท่านั้น ไม่รวม process หรือ host config
ของ service ซึ่งเป็นหน้าที่ของ `service remove` โดยเฉพาะ

เมื่อไม่มี service เหลือแล้ว ระบบจะถามทีละขั้น (เรียงลำดับ: Backups ->
Logs -> Web Data) โดย Backups/Logs แค่ "บันทึกการตัดสินใจ" ไว้ก่อน
(บอกทันทีว่าจะเก็บหรือลบ) ยังไม่ลบจริง ส่วน **Web Data ที่ถามเป็น
คำถามสุดท้ายคือจุดยืนยันของทั้งชุด**: ตอบ "n" ที่ Web Data จะยกเลิกทั้งหมด
(รวมถึง Backups/Logs ที่เพิ่งเลือกไปด้วย ไม่มีอะไรถูกลบเลย) ตอบ "y" จะลบ
ทั้งสามอย่างตามที่เลือกไว้ในคราวเดียว

## Service

```
wor service add <domain>/<service> [--host=<host>] [--port=<port>]
    [--entry=<entry-point>] [--service-type=static|node|go|python|php]
    [--php-version=<version>] [--no-php-pool] [--no-start]
wor service remove <domain>/<service> [--cascade] [--yes]
wor service start <domain>/<service>
wor service stop <domain>/<service>
wor service restart <domain>/<service>
wor service status
wor service logs <domain>/<service> [--lines=100]
```

`service add` จะ**บล็อกทันที**ถ้า runtime ของ template ที่เลือกยังไม่ได้
ติดตั้ง (ไม่มี prompt ถามว่า "ตั้งค่าให้เลยไหม") ให้ไปดู `wor doctor`
ว่าขาดอะไร service ที่เป็น `php` จะได้ php-fpm pool เฉพาะของตัวเองอัตโนมัติ
เมื่อเครื่องตรวจพบ PHP-FPM เพียงเวอร์ชันเดียว (`--php-version=` เลือกเมื่อ
เจอหลายเวอร์ชัน, `--no-php-pool` กลับไปใช้ endpoint ที่ใช้ร่วมกันแบบเดิม
-- ดูรายละเอียดเต็มใน `docs/services.md` และ `DESIGN.md` หัวข้อ 8)
service ที่เป็น `node`/`go`/`python` จะถูก start ให้อัตโนมัติทันทีหลังสร้าง
เสร็จ (เหมือนสั่ง `wor service start` ต่อท้ายให้เอง) ใส่ `--no-start` เพื่อ
ข้ามขั้นตอนนี้ (เช่น อยากตั้งค่า env/secret ก่อนแล้วค่อย start เอง) ถ้า
auto-start ล้มเหลว (เช่น runtime มีปัญหาชั่วคราว) จะแค่เตือน ไม่ทำให้
`service add` เอง fail -- เพราะ config/ecosystem/unit ถูกสร้างสำเร็จไปแล้ว
ก่อนหน้านั้น สั่ง `wor service start <domain>/<service>` เองอีกทีได้ทันที

`service remove` จะบล็อกถ้ายังมี host อ้างอิง service นี้อยู่ เว้นแต่ใส่
`--cascade` (จะลบ host config ที่เกี่ยวข้องไปด้วย)

`service status` ไม่ได้แค่เรียก `pm2 status` เหมือนเดิมอีกต่อไป --
จะรวบรวม service **ทุกตัว** (รวมที่ disabled) จากทุก domain มาแสดงเป็น
กลุ่มตาม process provider จริง (`PM2 (node)`, `SYSTEMD (go/python)`,
`PHP-FPM (php)`, `STATIC (no process)`) แต่ละแถวโชว์สถานะ
(online/pid/uptime/cpu%/memory) จาก `pm2 jlist`/`systemctl show` แบบ
query ครั้งเดียวสำหรับทุก service ในกลุ่มนั้น ไม่ query ซ้ำทีละตัว

สัญลักษณ์หน้าแถว (ตกลง 2026-07-06 หลังเหตุการณ์ "status เขียวหมดแต่
เว็บล่ม"): ✓ สีฟ้า = enabled, ✗ สีแดง = disabled -- icon สื่อ **config
state เท่านั้น** ตั้งใจไม่มีจุดเขียวเพราะเขียวถูกอ่านเป็น "เว็บดี" ซึ่ง
คำสั่งนี้ไม่เคยตรวจ ส่วน process state เป็นคอลัมน์ท้ายแถว โดยคำ state
ของ service ที่ enabled แต่ process ไม่ทำงาน (errored/stopped/not
started) เป็นตัวอักษรสีแดง แถว disabled แสดง state "disabled" แบบ dim
และไม่ถูก query ท้ายรายงานมี hint ชี้ไป `wor health` สำหรับ
end-to-end health เสมอ

`service start`/`stop`/`restart`/`logs` จะ error ทันทีถ้า domain/service
ที่ระบุไม่มีอยู่จริง (ไม่ fallback เงียบ ๆ ไปเป็น "static service ที่ไม่มี
อะไรต้องทำ" เหมือนที่เคยเป็นบั๊กมาก่อน)

## wor run

```
wor run
```

คำสั่งเดียวที่ตรวจสอบและสตาร์ท**ทุก service ที่ enabled ไว้ทั้งเครื่อง**
พร้อม runtime/web server ที่จำเป็น ตั้งใจตั้งชื่อว่า `run` ไม่ใช่
`start`/`up` เพราะเป็นคำสั่งทิศทางเดียว ("ทำให้ระบบอยู่ในสถานะที่ต้องการ"
คล้าย `terraform apply` หรือ `docker-compose up`) ไม่มี `wor stop`/
`wor down` คู่กันตามมา

ลำดับการทำงาน:
1. เช็คทีเดียวก่อนเข้า loop: web server provider ที่ active อยู่ (start
   ให้ถ้ายังไม่ทำงาน) และ pm2 daemon (เฉพาะถ้ามี service ที่ต้องใช้ pm2)
   -- ถ้า `pm2 startup` ยังไม่เคยถูกลงทะเบียนไว้บนเครื่องนี้เลย จะเสนอ
   ลงทะเบียนให้ทันที (บอกคำสั่งที่จะรันก่อนเสมอ แล้วขอสิทธิ์ sudo ผ่าน
   pattern confirm-once เดียวกับที่ privileged operation อื่น ๆ ใช้)
   เพื่อปิดช่องโหว่ที่ service ที่ใช้ pm2 ไม่เคยกลับมาทำงานเองหลัง reboot
2. วน loop ทีละ service ที่ enabled: เช็ค/สตาร์ท runtime ที่ service
   นั้นต้องใช้ก่อน (สำหรับ php ที่มี pool เฉพาะตัว) แล้วค่อยสตาร์ท
   service เอง ถ้ายังไม่ทำงาน

service ที่ล้มเหลวจะถูกข้ามไป ไม่ทำให้คำสั่งทั้งหมดหยุด ท้ายสุดจะสรุป
เป็นบรรทัดเดียวว่าสำเร็จกี่ service ล้มเหลวกี่ service

## Host

```
wor host add <host> [--target=<domain>/<service>] [--server=nginx|apache]
    [--replace] [--domain-type=local|public] [--add-hosts|--no-hosts]
wor host remove <host> [--yes]
wor host list
wor host test
wor host reload
wor host logs <host> [access|error] [--lines=100]
```

`host list` แสดงตารางเดียวใต้หัวรายงาน `WOR Hosts <server> (<version>)`
(ไม่มี group header ENABLED/DISABLED แล้ว) แต่ละแถวมี ✓ สีฟ้า =
enabled / ✗ สีแดง = disabled (เทียบ sites-available กับ sites-enabled;
แถว enabled เรียงก่อน), target (`domain/service`), port, และตัวอักษร
`ssl`/`-` แบบสีปกติ -- ตั้งใจไม่ใช้สีเขียวทั้งบรรทัด เพราะรายการนี้
รายงาน config ไม่ใช่ health (มี cert ในระบบไม่ได้แปลว่า cert ใช้ได้ --
`wor diagnose <host>` คือตัวตรวจจริง)

`host remove` จะลบทั้ง host config, entry ใน services.config.json,
entry ใน `/etc/hosts`, และ state SSL ที่บันทึกไว้ (`$WOR_HOME/ssl/hosts/
<host>/`) ให้ครบในคำสั่งเดียว

## Database

```
wor database add <domain>/<profile> [--label="Label"]
wor database remove <domain>/<profile>
wor database backup <domain>/<profile>[/database]
```

รองรับแค่ backup เท่านั้น **ไม่มี** restore/drop/migrate (ตั้งใจไม่ทำ
เหมือน wor-cli เวอร์ชันเดิม) `add` จะ error ถ้า domain ยังไม่มีอยู่จริง
(ไม่สร้าง domain ให้อัตโนมัติเหมือนที่เคยเป็น) โปรไฟล์ซ้ำจะไม่ error แต่
เตือนแทน `remove` จะลบทั้ง entry ใน config และไฟล์ `.env` ของโปรไฟล์นั้น
(เดิมเคยลืมลบไฟล์ `.env`)

## Source

```
wor source clone <domain> <git-url>
wor source clone <domain>/<service> <git-url>
wor source pull <domain> [--stash]
wor source pull <domain>/<service> [--stash]
wor source backup <domain> [--gitignore=enable|disable]
wor source backup <domain>/<service> [--gitignore=enable|disable]
```

`source clone` ถ้า target มี source อยู่แล้ว จะสำรอง (ผ่าน `wor source
backup`) แล้วแทนที่ให้อัตโนมัติเสมอ ไม่ต้องใส่ flag ใด ๆ เพิ่ม (เดิม
เคยต้องใส่ `--replace` แต่ตอนนี้ถือว่าเป็นพฤติกรรมที่ต้องการอยู่แล้ว
โดย backup คือ safety net) ระหว่างแทนที่ จะย้าย source เดิมไปพักไว้ก่อน
เสมอ ไม่ลบทิ้งตรง ๆ จนกว่าจะย้าย source ใหม่เข้าที่สำเร็จแล้วจริง ๆ

ถ้า tree เดิมมี `.env` จะเตือนแบบเด่น (clone ใหม่ไม่มี `.env` และ backup
zip ก็อาจไม่มีเพราะกรองตาม `.gitignore`) แล้วให้เลือก: **keep both**
(ค่าเริ่มต้น -- `.env` เดิมใช้งานต่อ ของ repo เก็บเป็น `.env.new`),
**overwrite** (`.env` เดิมใช้งานต่อ ทิ้งของ repo), หรือ **replace**
(ใช้ของ repo, ทิ้งของเดิม -- ต้องยืนยันซ้ำอีกชั้น)

clone เสร็จแล้ว ถ้า target เป็น service ที่ลงทะเบียนไว้ จะถามว่า deploy
เลยไหม (delegate ไป `wor deploy --no-pull --force`) เพราะ clone สด ๆ
ยังไม่มี `node_modules` / build output / go binary -- service ยังรัน
ไม่ได้จนกว่าจะติดตั้ง dependency และ build ก่อน

`source backup` บีบอัดเป็น `.zip` ผ่าน `archive/zip` ของ Go เอง (ไม่พึ่ง
โปรแกรม `zip` ภายนอก) ค่าเริ่มต้นจะกรองไฟล์ตาม `.gitignore` ที่ root ของ
source tree นั้นด้วย (นอกเหนือจาก exclude list เดิมใน
`backup.config.json`) `--gitignore=enable|disable` overrides พฤติกรรม
นี้แค่ครั้งเดียวโดยไม่แก้ config ตัว matcher จงใจอ่านแค่ `.gitignore`
ไฟล์เดียวที่ root เท่านั้น ไม่รองรับ `.gitignore` ซ้อนในโฟลเดอร์ย่อยแบบที่
git จริงทำ (trade-off ที่เลือกไว้เพื่อไม่ต้องเขียน matcher เต็มรูปแบบ)

## Deploy / Rollback

```
wor deploy <host|domain/service> [--pull-only] [--no-pull] [--no-restart] [--force] [--stash]
wor rollback <domain>/<service> [--yes]
```

`deploy` = pull โค้ดใหม่ (ถ้ามี) -> ติดตั้ง dependency ถ้าไฟล์ manifest
เปลี่ยน (`package.json`/`requirements.txt`) -> build ถ้าจำเป็น (node
เช็คจาก `npm run build` script, go build **ทุกครั้ง**ที่มี commit ใหม่
ไม่มี heuristic แบบ node) -> restart service ผ่าน process provider ที่
ถูกต้อง -> เช็คสุขภาพหลัง restart (PM2 `describe`/systemd `is-active`)

`--force` ข้ามการเช็ค "manifest เปลี่ยนไหม" ทั้งหมด: บังคับ `npm ci`
(หรือ `npm install` ถ้า repo ไม่มี lockfile), `npm run build`,
`pip install -r requirements.txt`, และ go build เสมอ -- นี่คือกลไกที่
`wor rollback` และ `wor source clone` ใช้ (เรียก deploy ด้วย
`--no-pull --force`) เพราะสองกรณีนั้น commit ไม่ได้ขยับจากมุมมองของ
deploy แต่ dependency คือสิ่งที่หายหรือ stale พอดี

`rollback` คือ hard-reset source กลับไปที่ `origin/<branch>` ทิ้งการ
แก้ไขที่ยังไม่ commit ทั้งหมด (สำรองผ่าน `wor source backup` ก่อนเสมอ)
รับเฉพาะ `domain/service` เท่านั้น ไม่รับ domain เปล่า ๆ

## SSL

```
wor ssl issue <host> [--provider=letsencrypt|self-signed|custom|none] [--preferred=<host>]
wor ssl renew <host>
wor ssl status <host>
wor ssl remove <host> [--yes]
wor ssl install <host> --cert=/path/fullchain.pem --key=/path/privkey.pem
```

`letsencrypt` (ผ่าน certbot) รองรับเฉพาะ Unix เท่านั้น (ไม่มี certbot
เวอร์ชันที่เชื่อถือได้บน Windows) `self-signed` (ผ่าน `openssl` ถ้ามี)
กับ `custom` (นำใบรับรอง/key ของตัวเองมาใช้) ใช้ได้ทุก OS

## Info

```
wor info <host|domain/service>
```

แสดงสรุปข้อมูลของ host หรือ domain/service ที่ระบุในคำสั่งเดียว: type,
enabled, source path, host ที่ผูกอยู่ (พร้อมสถานะ SSL ต่อ host),
สถานะ process จริงตาม provider ของ service นั้น (pm2 describe /
systemctl status / php-fpm pool socket+version+group แล้วแต่ type),
reachability check ว่า web server user (nginx/apache) traverse ผ่าน
WOR_HOME และ docroot ของ service นี้ได้ไหม (เฉพาะ Debian/Ubuntu, static/php
เท่านั้น -- node/go/python reverse-proxy ไม่ต้องอ่านไฟล์), section
Resources (host CPU%/Mem + cpu/mem ของ service นี้ -- รายละเอียดดูใน
`wor health` ด้านล่าง), และ git status ถ้า source เป็น git repo

## Health

```
wor health
```

โหมด "เว็บล่มแต่ยังไม่รู้ตัวไหน" (เช่นหลังเครื่อง reboot): กวาดทุก
service ที่ enabled -- เช็คชั้น process/port ต่อตัว **แล้วยิง HTTP
จริงผ่าน web server หนึ่ง request ต่อ service** (host แรกที่ลงทะเบียน,
dial 127.0.0.1 + Host header) เพราะปัญหา permission/vhost/proxy
ไม่เคยฆ่า process -- "pool accepting" ไม่ได้แปลว่าเว็บเข้าได้
(บทเรียนจากเหตุการณ์จริง 2026-07-06)

Layout เป็นการ์ดต่อ service (mockup จาก owner, ตกลง 2026-07-07):
ส่วนหัวสรุปเครื่อง `Host CPU` (% รวมทุก core, sample `/proc/stat`
สองครั้ง) / `Host Memory` (`/proc/meminfo` แบบ MemTotal−MemAvailable) /
`Disk Usage` (filesystem ของ WOR_HOME) แล้วตามด้วยการ์ด ● ต่อ service:
Status, Runtime (พร้อมจำนวน php-fpm workers สำหรับ dedicated pool),
CPU (100% = 1 core เต็ม แบบ top), Memory (RSS + % ของ RAM ทั้งเครื่อง),
Uptime (เฉพาะ pm2) และบรรทัด HTTP `✓/⚠/✗ <url> -> <code>` -- บรรทัด
CPU/Memory/Uptime ที่ไม่มีข้อมูล (static ฯลฯ) ซ่อนทั้งบรรทัด ไม่แสดง "-"

สถานะมี 3 ระดับ: ● เขียว = healthy, ● เหลือง = **Warning** (HTTP 404
"may be normal for APIs" หรือไม่มี host ให้ probe -- แสดงให้เห็นแต่
exit code ยังเป็น 0 เพื่อไม่ให้ cron/monitoring เด้งเก้อ), ● แดง =
FAILED (process พัง หรือ HTTP 4xx/5xx/refused/timeout) ท้ายรายงาน
สรุปนับ Healthy/Warning/Failed พร้อม `wor diagnose <target>` ต่อตัว
ที่พัง ที่มาข้อมูล resource ต่อ provider: pm2 = monit จาก `pm2 jlist`,
systemd = CPUUsageNSec/MemoryCurrent delta (`GetInfoBatch`), php pool
= รวม worker ทุกตัวใน `/proc` (จับคู่ title "php-fpm: pool <name>")
sample ทั้งรายงานรอครั้งเดียว (~200-250ms) ตัวอ่าน `/proc` เป็น
Linux-only (macOS เห็นเฉพาะค่าจาก pm2)

เดิมคือ `wor diagnose --all` -- แยกออกมาเป็นคำสั่งของตัวเอง (ตกลง
2026-07-06) เพราะ "diagnose" คือการวิเคราะห์ผู้ป่วยที่รู้ตัวแล้ว ส่วน
คำสั่งนี้คือการ*หา*ว่าใครป่วย เส้นแบ่งกับ `wor doctor`: doctor ตอบ
"เครื่อง/runtime ติดตั้งพร้อมไหม", health ตอบ "service ยังเสิร์ฟได้
ไหม" ข้อกำหนดเดียวกับ diagnose ทุกข้อ: read-only, ไม่มี sudo prompt,
ทุก probe มี timeout, exit code 0/1 ใช้กับ cron/monitoring ได้ และ
ไม่ถือ WOR_HOME lock

เรื่องเล่าครบวง: `wor health` (ใครพัง) -> `wor diagnose <target>`
(พังเพราะอะไร + แก้ยังไง) -> `wor run` (เอากลับมา)

## Diagnose

```
wor diagnose <host|domain/service>
```

วิเคราะห์หา root cause ของ service ที่ล่ม/มีปัญหา แบบ read-only ล้วน ๆ
(**ไม่ auto-fix ใด ๆ ทั้งสิ้น** -- แสดงคำสั่งแก้ให้ copy-paste เท่านั้น
admin เป็นผู้ตัดสินใจและรันเอง ดู design เต็มใน `docs/diagnose.md`)
ตรวจเป็นชั้นจากนอกเข้าในตาม request path: config (enabled/entry
point/docroot/runtime) -> dns/hosts file -> web server (รันอยู่ไหม,
vhost มี+enabled, config test แบบ unelevated) -> SSL (ไฟล์ cert +
วันหมดอายุ อ่านด้วย crypto/x509 ไม่พึ่ง openssl) -> process
(pm2/systemd/php-fpm ตาม provider พร้อม crash-loop detection และ
กรณีพิเศษ "pm2 ว่างหลัง reboot") -> port (แยก "ไม่มีใครฟัง" กับ
"process อื่นแย่ง port") -> HTTP probe สองชั้น (ยิงตรงเข้า app
และยิงผ่าน web server ที่ 127.0.0.1 พร้อม Host header จึงไม่หลงไป
CDN/proxy) -> file reachability (Debian/Ubuntu) -> disk ->
logs (pm2/journalctl/nginx error log พร้อม pattern ที่รู้จัก เช่น
EADDRINUSE, Cannot find module, OOM) ท้ายรายงานสังเคราะห์ทุก FAIL
เหลือ **Root cause เดียว + Evidence (จัดกลุ่ม/ยุบซ้ำ) + Fix** --
FAIL ที่เป็นปัญหาเดียวกันจากคนละชั้น (เช่น http 403 + file
permission) ถูก merge ด้วยระบบ kind+confidence ไม่ปล่อยให้ผู้ใช้
ตีความเอง มี "Other possibilities" ไม่เกิน 2 รายการกัน ranking
เดาผิด และถ้า source เพิ่งเปลี่ยนไม่ถึงชั่วโมงจะแนะ `wor rollback`
ให้ด้วย ส่วนหัวรายงานสรุป Target/Host/Runtime/Server ในบรรทัดแรก ๆ

เพิ่มเติมจากบทเรียนหน้างานจริง (2026-07-06): (1) ชั้น process ของ php
pool เช็ค **permission ของ socket จากมุมของ web server user** ด้วย --
pool ที่ตอบ wor ได้แต่ socket ไม่ยอมให้ www-data connect (listen.owner
ผิด) เคยขึ้น PASS ทั้งที่ 502 จริง ตอนนี้เป็น FAIL พร้อมคำสั่ง sed แก้
pool config (2) log evidence จาก nginx/apache error log **กรองทิ้ง
บรรทัดที่เก่ากว่า 1 ชั่วโมง** (parse timestamp ได้ไม่ครบให้ผ่าน ไม่ตัด)
เพราะ http probe เพิ่งยิงไปเมื่อครู่ ปัญหาที่ยังอยู่จริงย่อมมีบรรทัดใหม่
เสมอ -- กันบรรทัดค้างจากก่อนแก้ config มา hijack root cause (3) ถ้า log
อ้าง path ที่มี `/domains/` แต่ไม่อยู่ใต้ WOR_HOME ปัจจุบัน จะฟันธงเป็น
"config จาก installation เก่ายัง active" แทน permission ทั่วไป

สำหรับการกวาดหา service ที่พังทั้งเครื่อง ดู `wor health` (เดิมคือ
`wor diagnose --all` ที่ถูกแยกออกไปเป็นคำสั่งของตัวเอง)

ข้อกำหนดเชิงพฤติกรรม (ใช้ร่วมกับ `wor health` ทุกข้อ): ไม่มี prompt
ขอ sudo เด็ดขาด (เช็คไหนต้องการ root จะรายงานว่า "not verified" แทน)
ทุก probe มี timeout สั้น และ exit code เป็น 0 เมื่อไม่พบปัญหา / 1
เมื่อพบ จึงใช้กับ cron/monitoring ได้ และไม่ถือ WOR_HOME lock
(เช่นเดียวกับ version/help/logs และ path/shell-init) -- เครื่องมือ
วินิจฉัยต้องไม่ถูกบล็อกตอนเกิดเหตุ

## Path / Goto (นำทางเข้าโฟลเดอร์)

```
wor path [.|./<path>|<domain>[/<service>]]
wor shell-init
wor goto [.|./<path>|<domain>[/<service>]]   (shell function)
```

- `wor path <domain>` / `wor path <domain>/<service>` -- resolve เป็น
  directory ใต้ `WOR_HOME/domains` แล้วพิมพ์ **path เปล่า ๆ บรรทัดเดียว**
  ออก stdout (ไม่มี prefix `[OK]`) เพื่อให้ `cd "$(wor path myapp/backend)"`
  ใช้ได้ตรง ๆ error ทั้งหมดออก stderr + exit 1
- `wor path .` -- WOR_HOME เอง / `wor path ./<path>` -- `WOR_HOME/<path>`
  (subtree ใดก็ได้ เช่น `./logs`, `./backups/myapp`) รูปแบบ `./` รับ path
  หลายชั้นจึงไม่ผ่านกฎ slug -- ปิด traversal แทนด้วย `filepath.Clean`
  แล้วปฏิเสธทุกอย่างที่ยังขึ้นต้น `..` หรือกลายเป็น absolute path
- validation เป็นแค่ "directory มีจริงบนดิสก์" (os.Stat) ไม่เช็ค
  services.config.json -- โฟลเดอร์ที่ยังไม่ได้ลงทะเบียนเป็น service
  ก็ยังนำทางเข้าไปดูได้
- **ไม่ใส่ argument** (ทั้ง `wor path` และ `wor goto`) -- ขึ้นเมนูเลขให้เลือก:
  รายการแรกเป็น `WOR_HOME (<path จริง>)` เสมอ ตามด้วยทุก domain และ
  domain/service เรียงตามชื่อ เมนู/prompt ออก stderr เลือกด้วยตัวเลขจาก
  stdin แล้วพิมพ์เฉพาะ path ที่เลือกออก stdout -- สัญญา 3 stream นี้ทำให้
  เมนูทำงานได้แม้อยู่ใน command substitution ของ shell function
  กด Enter เปล่า = ยกเลิก (exit 1 -- ห้ามเป็น 0 ไม่งั้น shell จะ `cd ""`)
- `wor shell-init` -- พิมพ์ shell function สำหรับติดตั้งใน rc file:
  `eval "$(wor shell-init)"` ใน `~/.bashrc`/`~/.zshrc` แล้วได้
  `wor goto <target>` ที่ **cd ไปเลย** (process เปลี่ยน cwd ของ shell แม่
  ไม่ได้ -- แนวเดียวกับ zoxide/nvm จึงต้องเป็น shell function ที่ห่อ
  `cd "$(command wor path ...)"`) `install.sh` เสนอเพิ่มบรรทัดนี้ให้
  อัตโนมัติตาม login shell ของ operator
- ทั้งคู่ read-only และไม่ถือ WOR_HOME lock (`path` ถูกเรียกทุกครั้งที่
  `goto`, `shell-init` ถูก eval ทุกครั้งที่เปิด shell ใหม่ -- ห้ามไปต่อคิว
  หลัง deploy ที่กำลังรัน) `shell-init` ยังได้รับยกเว้นจาก
  workspace-init gate ด้วย: ถ้ามันพิมพ์ ERROR ตอน workspace ยังไม่
  initialize ข้อความนั้นจะถูก eval เป็นคำสั่ง shell ในทุก terminal ใหม่

## Environment variables

`wor` แสดงค่าที่กำลังใช้งานอยู่ท้าย `wor help`/`wor <ไม่ระบุคำสั่ง>`
เสมอ: `WOR_ENV`, `WOR_HOME`, และไฟล์ config ที่ใช้อยู่ ตัวแปรเหล่านี้
กำหนดผ่าน `wor setup` หรือแก้ตรงที่ config file/`host.env` ได้เอง
