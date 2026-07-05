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
- `wor doctor` -- ตรวจสุขภาพระบบแบบ read-only แสดง checklist
  ✓/⚠/✗ ของ environment, runtime (Node.js/Go/Python/PHP/PHP-FPM),
  web server ที่ active, ฐานข้อมูล (optional เสมอ), และเครื่องมืออื่น ๆ
  (git/zip/gzip, optional เสมอ) exit code ไม่เป็นศูนย์เมื่อมีอะไรที่
  "จำเป็น" ขาดหายไป (runtime หลัก, host provider ที่ตั้งค่าไว้แต่ไม่ได้
  ติดตั้ง, หรือ workspace ยังไม่ initialize)
- `wor env` -- แสดงค่า config/environment ปัจจุบัน
- `wor clean` -- ลบ host config/PM2 process/systemd unit/`/etc/hosts`
  entry ที่กลายเป็น orphan (ไม่มี domain/service อ้างอิงอยู่แล้ว) แต่ไม่แตะ
  อะไรที่ยังผูกกับ service ที่ registered อยู่
- `wor reset` -- ล้างทุกอย่างที่ wor สร้างไว้กลับสู่สภาพเริ่มต้น
  (PM2 process ที่ขึ้นต้นด้วย `wor_`, systemd unit `wor_*.service`, host
  config `wor__*.conf`, entry ใน `/etc/hosts`, โฟลเดอร์ domains/backups/
  logs) **ไม่ลบ** host config อื่นที่ไม่ใช่ของ wor ต้องพิมพ์ `RESET`
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
    [--php-version=<version>] [--no-php-pool]
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

`service remove` จะบล็อกถ้ายังมี host อ้างอิง service นี้อยู่ เว้นแต่ใส่
`--cascade` (จะลบ host config ที่เกี่ยวข้องไปด้วย)

`service status` ไม่ได้แค่เรียก `pm2 status` เหมือนเดิมอีกต่อไป --
จะรวบรวม service ที่ enabled ทุกตัวจากทุก domain มาแสดงเป็นกลุ่มตาม
process provider จริง (`PM2 (node)`, `SYSTEMD (go/python)`,
`PHP-FPM (php)`, `STATIC (no process)`) แต่ละแถวโชว์สถานะ
(online/pid/uptime/cpu%/memory) จาก `pm2 jlist`/`systemctl show` แบบ
query ครั้งเดียวสำหรับทุก service ในกลุ่มนั้น ไม่ query ซ้ำทีละตัว

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

`host list` จัดกลุ่มเป็น ENABLED/DISABLED (เทียบ sites-available กับ
sites-enabled) แสดง target (`domain/service`), port, และ badge SSL ของ
แต่ละ host ด้วย

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

แสดงสรุปข้อมูลของ host หรือ domain/service ที่ระบุในคำสั่งเดียว

## Environment variables

`wor` แสดงค่าที่กำลังใช้งานอยู่ท้าย `wor help`/`wor <ไม่ระบุคำสั่ง>`
เสมอ: `WOR_ENV`, `WOR_HOME`, และไฟล์ config ที่ใช้อยู่ ตัวแปรเหล่านี้
กำหนดผ่าน `wor setup` หรือแก้ตรงที่ config file/`host.env` ได้เอง
