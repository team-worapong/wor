# บันทึกการออกแบบ: wor-cli (bash) -> wor (Go)

เอกสารนี้บันทึกความแตกต่างที่ตั้งใจทำจาก shell CLI ตัวเดิม พร้อมเหตุผล
ของแต่ละจุด สิ่งใดที่ไม่ได้พูดถึงในนี้ควรมีพฤติกรรมเหมือนเดิมทุกประการ
(directory convention เดิม, ชื่อ PM2 แบบเดิม `wor_<domain>_<service>`,
ชื่อไฟล์ host แบบเดิม `wor__<host>.conf` / `000_wor_default.conf`,
ตัวแปร template เดิม)

หัวข้อ 1-8 คือการออกแบบตั้งต้นตอนพอร์ตจาก bash มาเป็น Go หัวข้อ 9 เป็นต้นไป
คือฟีเจอร์/การรีดีไซน์ที่เพิ่มเข้ามาทีหลัง หลังจากพอร์ตเสร็จรอบแรกแล้ว

## 1. ไฟล์ config เป็น JSON ไม่ใช่ JS ที่เขียนเอง

shell CLI เก็บ `services.config.js` / `databases.config.js` /
`backup.config.js` เป็นไฟล์ `module.exports = {...}` อ่าน/เขียนด้วยการ
shell out ไปเรียก `node -e '...'` ทำให้ Node.js กลายเป็น hard dependency
สำหรับแค่ "จัดการ config" แม้แต่เว็บ static ที่ไม่มี Node.js service เลย
ก็ตาม และวิธีนี้ไม่มีทางเทียบเท่าที่ใช้งานได้ดีบน Windows โดยไม่ต้องสมมติ
ว่า Node อยู่ใน PATH ตั้งแต่ก่อนที่ wor เองจะ list domain ได้ด้วยซ้ำ

เวอร์ชัน Go เก็บข้อมูลชุดเดียวกันเป็น `services.config.json` /
`databases.config.json` / `backup.config.json` อ่าน/เขียนด้วย
`encoding/json` โครงสร้างและ field เดิมทุกอย่าง (`domain`,
`services[].name/type/hosts/port/...`) เปลี่ยนแค่นามสกุลไฟล์ ไม่มีขั้นตอน
generate โค้ดอีกต่อไป ถ้ามีไฟล์ `*.config.js` เดิมจาก wor-cli v1 อยู่
ต้องแปลงเป็น `.json` เอง (ตัด `module.exports = ` ข้างหน้ากับ `;` ท้าย
ไฟล์ออก) เวอร์ชันนี้ยังไม่มี migration อัตโนมัติ

`wor.config.js` (ไฟล์ PM2 ecosystem ที่ generate ออกมา) เปลี่ยนเป็น
`wor.config.json` เช่นกัน PM2 รองรับ `pm2 start ecosystem.json` เป็นค่า
built-in อยู่แล้ว เปลี่ยนแบบนี้ไม่เสียความสามารถอะไรไปเลย

## 2. ไม่ shell out ไปเรียก gzip/zip/tail/ss/lsof/netstat

shell เวอร์ชันเดิมประกอบด้วย Unix utility เล็ก ๆ หลายสิบตัว แต่ละตัวคือ
จุดที่การพอร์ตไป Windows จะพัง เวอร์ชัน Go แทนที่ด้วย standard library:

- บีบอัด backup ฐานข้อมูล: `compress/gzip` แทนการ pipe ผ่านโปรแกรม
  `gzip`
- backup source: `archive/zip` แทนการ shell out ไป `zip`
- เช็คว่า port ว่างไหม (auto-port picker ของ `wor service add`): ลอง
  `net.Listen("tcp", ...)` แทนการ parse output ของ `ss`/`lsof`/`netstat`
- `wor host logs`: เขียน tail-and-follow loop เองเล็ก ๆ แทน `tail -f`
  (ซึ่งไม่มีบน Windows)

## 3. Path ของ host provider ต่างกันตาม OS

nginx/apache มี convention เรื่อง sites-available/sites-enabled/log
directory ต่างกันตาม OS:

- Linux: `/etc/nginx/sites-available` + `/etc/nginx/sites-enabled`
  (แบบ Debian ดั้งเดิม), `/etc/apache2/sites-available` หรือ
  `/etc/httpd/conf.d`
- macOS (Homebrew): ใช้ directory เดียวแบบ flat (`servers/` สำหรับ
  nginx, `servers/` ใต้ `httpd` สำหรับ apache) -- ไม่มี directory
  "enabled" แยกต่างหาก การ enable host จึงเป็น no-op ทันทีที่เขียนไฟล์
  เสร็จ
- Windows: ไม่มี convention มาตรฐานที่ใช้กันทั่วไป เวอร์ชันนี้ตั้งค่า
  default เป็น `C:\nginx\conf\sites-available` /
  `C:\Apache24\conf\sites-available` โดย directory "enabled" เท่ากับ
  directory "available" เลย (โมเดล flat-directory แบบเดียวกับ macOS
  เพื่อเลี่ยงปัญหาที่การสร้าง symlink บน Windows ต้องมีสิทธิ์
  Administrator หรือเปิด Developer Mode) ค่าเหล่านี้เป็นแค่ default ที่
  สมเหตุสมผล ไม่ใช่ค่าที่ถูกต้องสากล -- override ผ่าน `host.env`
  (`NGINX_SITES_AVAILABLE=` ฯลฯ) ให้ตรงกับ nginx/Apache จริงบนเครื่อง
  Windows นั้น

ทั้งหมดนี้อยู่หลัง interface เดียว (`internal/hostprovider`) เพิ่ม
default ที่แม่นยำกว่าสำหรับ Windows ทีหลังได้โดยไม่ต้องแตะโค้ดคำสั่งใด ๆ

## 4. การขอสิทธิ์ (Privilege elevation)

ฝั่ง Unix ใช้โมเดลเดิม: ถ้าไม่ใช่ root ให้ครอบ operation ที่ต้องใช้สิทธิ์
(`mkdir`, เขียนเข้า `/etc/nginx/...`, `tee`, `rm`, `ln`, `systemctl
reload`, `certbot`) ด้วย `sudo` แต่เพิ่ม 2 อย่างจากเวอร์ชัน shell:

- **`wor` ปฏิเสธการรันแบบ `sudo wor ...`** `osutil.IsSudoElevated()`
  เช็คทั้ง root *และ* มี environment variable `SUDO_USER` (ซึ่ง `sudo`
  ตั้งให้ child process เสมอ แต่การ login เป็น root ตรง ๆ จะไม่มี)
  `App.Run()` เช็คจุดนี้ก่อนส่งต่อไป subcommand ใด ๆ เลย ถ้าเจอจะ error
  ทันที การเช็คแบบนี้แคบกว่า "reject ถ้าเป็น root" โดยตั้งใจ: server ที่
  ไม่มี user account อื่นนอกจาก root (login แล้วรัน `wor` ตรง ๆ ไม่ผ่าน
  `sudo` เลย) จะไม่ได้รับผลกระทบ เพราะ `SUDO_USER` จะไม่ถูกตั้งในกรณีนี้
  PM2 เองก็ปฏิเสธการรันภายใต้ sudo อยู่แล้ว (ดู `internal/pm2`) ส่วนนี้
  ปิดช่องโหว่เดียวกันให้ทุก subcommand เพื่อไม่ให้ user เผลอได้ git
  clone/npm install/PM2 dump ที่เป็นของ root มาโดยแค่เติม `sudo` หน้า
  คำสั่งทั้งหมด
- **`osutil.SudoCommand` จะถามยืนยันแค่ครั้งแรก (ต่อ 1 process) ที่ต้อง
  เติม `sudo` จริง ๆ** ไม่ถามล่วงหน้า และไม่ถามซ้ำสำหรับที่เหลือของคำสั่ง
  เดียวกัน `cliapp.New()` ต่อสายกลไกนี้เข้ากับ prompt แบบ `[Y/n]`
  (`osutil.SetElevationPrompt`) ถ้าตอบปฏิเสธ operation ที่ต้องใช้สิทธิ์
  ทุกอันหลังจากนั้นในคำสั่งเดียวกันจะ error ทันที ไม่ถามซ้ำอีก
  environment ที่ path ที่เกี่ยวข้องเขียนได้โดยไม่ต้องใช้สิทธิ์อยู่แล้ว
  (เช่น directory ของ nginx ที่ติดตั้งผ่าน Homebrew บน macOS) จะไม่เจอ
  prompt นี้เลย เพราะการเขียนแบบไม่มีสิทธิ์พิเศษสำเร็จตั้งแต่รอบแรก
  ไม่มีทางไปถึง `SudoCommand`

Windows ไม่มีกลไกรันคำสั่งซ้ำแบบขอสิทธิ์เพิ่มจากใน process ที่รันอยู่แล้ว
เวอร์ชันนี้จึงไม่ได้สร้าง flow เปิด UAC ใหม่ให้ -- การเปิด console แบบ
Administrator ยังเป็นวิธีเดียวที่จะรันคำสั่งที่ต้องใช้สิทธิ์บน Windows
`IsSudoElevated()` คืนค่า `false` เสมอบน Windows โดยตั้งใจ เพื่อไม่ให้
user บน Windows ถูกบล็อกแบบเดียวกับที่ `sudo wor` ถูกบล็อกบน Unix
`osutil.IsElevated()` เช็ค console ที่ elevated แล้วผ่าน `net session`
(สำเร็จเฉพาะ Administrator) เมื่อการเขียนที่ต้องใช้สิทธิ์ล้มเหลว
error message จะบอก user ให้เปิด terminal ใหม่แบบ Administrator แทนที่
จะพยายาม auto-elevate ผ่าน UAC prompt เงียบ ๆ (ซึ่งจะพังอยู่ดี)

## 5. SSL: Let's Encrypt รองรับเฉพาะ Unix

Certbot ไม่มีเวอร์ชัน official ที่เชื่อถือได้บน Windows `wor ssl issue
--provider=letsencrypt` จะ error ชัดเจนบน Windows ชี้ไปที่ `self-signed`
หรือ `custom` แทนที่จะพยายามทำอะไรที่เปราะบาง self-signed (ผ่าน
`openssl` ถ้าติดตั้งไว้) กับ custom (เอา cert/key ของตัวเองมาใช้)
ใช้งานได้ทุก OS

## 6. Service template: เพิ่ม go/python + systemd (ใหม่เทียบกับ v1)

wor-cli v1 ไม่มี template `go` หรือ `python` เวอร์ชันนี้เพิ่มเข้ามา
(ดู `docs/services.md`) พร้อมกับการจัดระเบียบครั้งใหญ่: ตัด template
แบบผสม 4 ตัว (`static-node`, `node-web`, `node-php`, `php-node`) ออก
เหลือแค่ 5 ตัว: `static`, `node`, `go`, `python`, `php` -- service หนึ่ง
ตัวคือ runtime หนึ่งชนิด ไม่ใช่การผสมกัน (กรณีที่ template ผสมเคยรองรับ
ตอบโจทย์ได้ดีกว่าด้วยการแยกเป็น static service กับ process-backed
service คนละตัวภายใต้ domain เดียวกัน)

ตอนนี้มี process supervisor 2 ตัว:

- **node** ใช้ PM2 เสมอ (เหมือน v1) ทุก OS
- **go** กับ **python** ใช้ **systemd** บน Linux (มีอยู่แล้วในแทบทุก
  distro และเข้าใจง่ายกว่าการเพิ่ม process manager ตัวที่สองที่ใช้ PM2)
  แล้ว fallback ไปเป็น **PM2** บน macOS และ Windows ที่ไม่มี systemd
  `domainmodel.ProcessProviderFor` คือจุดเดียวที่ตัดสินใจเรื่องนี้ตาม OS
  `internal/systemd` เลียนแบบโครงสร้างของ `internal/pm2` (generate unit,
  start/stop/restart/status/logs, ตั้งชื่อ `wor_<domain>_<service>`
  เหมือนกัน) ทำให้ทั้งสอง provider ใช้งานจาก CLI แทบไม่ต่างกัน
- **static** ไม่มี process ให้ดูแลเลย
- **php** ไม่มี *process* ให้ดูแล (php-fpm master สมมติว่า start ไว้เป็น
  system service ของตัวเองอยู่แล้ว) แต่หลังจากฟีเจอร์ per-service
  php-fpm pool (หัวข้อ 8) wor จะดูแลสิ่งหนึ่งใต้ master นั้น: ไฟล์
  `.conf` ของ pool เฉพาะ service ที่ wor เขียน/ลบ validate และสั่ง
  reload php-fpm เอง wor ยังไม่เคย start/stop/restart ตัว master
  process ของ php-fpm เอง -- แค่เพิ่ม/ลบไฟล์ pool ใต้มันเท่านั้น
  เหมือนกับที่ `wor host reload` แค่สั่งให้ nginx/apache reload ไม่เคย
  ดูแลมันในฐานะ process

ทุกการเรียก systemctl/journalctl ผ่าน confirm-once sudo gate เดียวกับที่
อธิบายในหัวข้อ 4

`go` มีขั้นตอนเพิ่มที่ node กับ python ไม่มี คือต้อง build:
`wor service add --service-type=go` กับ `wor create` จะรัน `go build`
ทันทีหลัง scaffold เสร็จ และ `wor deploy` จะรันซ้ำทุกครั้งที่ `git pull`
ดึง commit ใหม่มา (ไม่มีเงื่อนไข ไม่ได้อิงจาก diff แบบ heuristic ของ
node ที่เช็คจาก package.json เพราะแค่แก้ไฟล์ `.go` โดยไม่มี dependency
เปลี่ยนก็ต้อง compile ใหม่อยู่ดี)

`wor create` เปลี่ยนรูปแบบด้วยในการจัดระเบียบครั้งนี้: ไม่รับ flag
`--` ใด ๆ เลย (รับแค่ host เป็น positional argument ที่ไม่บังคับ)
ตอกย้ำความตั้งใจเดิมว่าเป็น "interactive only" flag เดียวที่เอาความ
สามารถจริงออกไปคือ `--domain=` (override domain id ที่ auto-derive มา)
กลายเป็น prompt แบบ confirm/override แทนที่จะหายไปเฉย ๆ งาน automation
ยังไปทาง `wor domain/service/host add` เหมือนเดิม ซึ่งได้ `--service-type=`
เพิ่มมา (เปลี่ยนชื่อจาก `--template=` ให้ตรงกับ `--domain-type=` ที่มีอยู่
แล้วและตรงกับชื่อ field `Service.Type` ภายใน) กับ flag ใหม่ `--entry=`
สำหรับ override ชื่อไฟล์/binary entry point ของ service

`wor create`/`wor service add` จะบล็อกการสร้าง service ทันทีด้วย error
ชัดเจนว่า "runtime not found" ถ้า runtime ของ template ที่เลือกยังไม่ได้
ติดตั้ง -- ตั้งใจไม่มี prompt แบบ "ตั้งค่าให้เลยตอนนี้ไหม" เหมือน wizard
อื่นบางตัวใน CLI นี้ `wor doctor` คือจุดเดียวที่รายงานว่าอะไรขาดและแก้
ยังไง

## 7. สิ่งที่ตั้งใจไม่ทำ (เหมือน v1)

- ไม่มี restore/drop/migrate สำหรับฐานข้อมูล -- backup อย่างเดียว
- `wor create` ยังคง interactive-only เท่านั้น งาน automation ไปทาง
  `wor domain/service/host add`
- Template เปลี่ยนไม่ได้หลังสร้าง service แล้ว (immutable)

## 8. Per-service php-fpm pool

ออกแบบและตกลงขอบเขตก่อนเริ่มเขียนโค้ด (ตาม convention ของโปรเจกต์ที่ต้อง
คุย/ยืนยัน design ก่อนสำหรับการเปลี่ยนแปลงที่มีผลต่อ architecture) เดิม
php service ทุกตัวใช้ `PHP_FPM_ENDPOINT` เดียวร่วมกันทั้งโฮสต์ (ค่า config
เดียว หรือ socket ที่ auto-detect จาก candidate list คงที่ --
`internal/hostprovider/phpfpm.go`) ตั้งแต่ฟีเจอร์นี้เป็นต้นไป php service
แต่ละตัวสามารถมี pool ของตัวเองผ่าน `internal/phpfpm`:

- **การแยกตัว (isolation)**: unix socket ของตัวเอง, ค่า `pm.*` ของ
  ตัวเอง, เวอร์ชัน PHP-FPM ของตัวเอง **การแยก unix user ต่างกันตาม OS**
  (แก้ไขจาก design ตั้งต้น -- ดูรายละเอียดด้านล่าง): บน Linux แต่ละ pool
  มี unix user เฉพาะของตัวเอง (สร้างผ่าน `useradd --system
  --no-create-home`) user นี้จะถูกเพิ่มเข้ากลุ่มเจ้าของเดิมของ document
  root ของ service แล้วให้สิทธิ์ `chmod g+rX` อ่านได้ -- ไม่มีการ chown
  เจ้าของ document root เดิมเลย บน **macOS ทุก pool ใช้ user เดียวกับที่
  รัน php-fpm master (ไม่มี unix user แยกต่อ service อีกต่อไป)**
- **ขอบเขต platform**: Linux (โครงสร้าง `/etc/php/<version>/fpm` แบบ
  Debian/Ubuntu) และ macOS (Homebrew ทั้ง formula ที่ตั้งชื่อเวอร์ชัน
  `php@<version>` และ formula `php` เฉย ๆ ที่เป็นเวอร์ชันปัจจุบันโดยไม่มี
  การตั้งชื่อเวอร์ชันแยก) เท่านั้น Windows ยังใช้พฤติกรรมเดิม (endpoint
  TCP แบบ global ตัวเดียว) ไม่เปลี่ยน -- PHP-FPM ไม่มีเวอร์ชัน official
  บน Windows เลยไม่มี pool ในเครื่องให้ wor จัดการ Linux สาย RHEL ใช้
  โครงสร้าง package ต่างจาก `/etc/php/<version>/fpm` และยังไม่รองรับ
  auto-detect (`phpfpm.DetectVersions`)
- **Lifecycle**: wor เขียนไฟล์ `.conf` ของ pool, validate config ที่ได้
  ด้วย `php-fpm -t` *ก่อน* แตะอะไรที่ทำงานจริง แล้วค่อย reload php-fpm
  (`systemctl reload phpX.Y-fpm` บน Linux, `brew services restart
  php@X.Y` บน macOS -- LaunchAgent wrapper ของ Homebrew ไม่มีคำสั่ง
  reload) เฉพาะตอนที่ validate ผ่านเท่านั้น ถ้า validate ไม่ผ่านจะ
  rollback ไฟล์ pool กลับ ไม่ปล่อยให้ config ที่ผิดค้างไว้ให้การ reload
  จริงครั้งถัดไปสะดุด
- **Backward compat / ไม่บังคับ migrate**: `domainmodel.Service.PHPVersion`
  จะว่างเปล่าสำหรับ php service ทุกตัวที่มีอยู่ก่อนฟีเจอร์นี้ และจะว่าง
  ต่อไปจนกว่าจะมีการสร้าง pool เฉพาะตัวให้จริง ๆ ค่าว่างหมายถึง "ใช้
  `PHP_FPM_ENDPOINT` แบบเดิมร่วมกันทั้งโฮสต์" -- ตอน render host config
  (`cliapp.buildWriteParams`) เช็คจาก field นี้ตรง ๆ php service ใหม่จะ
  ได้ pool เฉพาะตัวอัตโนมัติเมื่อเครื่องตรวจพบ PHP-FPM เพียงเวอร์ชันเดียว
  `--php-version=` เลือกเมื่อเจอหลายเวอร์ชัน และ `--no-php-pool` กลับไป
  ใช้ endpoint ร่วมกันแบบเดิมโดยตั้งใจ

### แก้ไข design 2026-07-05: ยกเลิกการแยก unix user บน macOS

พบผ่านการทดสอบจริงบนเครื่อง macOS ของ user (ตอนรัน `wor run` กับ php
service ที่มีอยู่แล้ว): design ตั้งต้นที่ว่า "แยก unix user เต็มรูปแบบทั้ง
Linux และ macOS" ทำไม่ได้จริงบน macOS เพราะ php-fpm master ที่รันผ่าน
Homebrew (`brew services start`) รันเป็น login user ปกติ ไม่ใช่ root
และ process ที่ไม่ใช่ root จะ `chown()` socket ให้เป็นของ unix user อื่น
หรือสลับ worker ไปรันเป็น user อื่นไม่ได้เลย -- การพยายามทำแบบนั้นทำให้
เจอ error จริง `failed to chown() the socket` ตอน pool ถูกใช้งานจริง
เป็นครั้งแรก

นำเสนอทางเลือกให้ user 2 ทาง (ยก php-fpm master บน macOS ให้รันเป็น root
เพื่อรักษาการแยกสิทธิ์ไว้ กับ ยกเลิกการแยก unix user บน macOS อย่างเดียว)
**user เลือกยกเลิกการแยกสิทธิ์บน macOS** Linux ไม่ได้รับผลกระทบ (systemd
รัน php-fpm เป็น root อยู่แล้ว การแยก unix user ต่อ service ยังทำงานตาม
design เดิมทุกประการ)

ผลคือ pool บน macOS ทุกตัวตอนนี้รันเป็น login user เดียวกับ php-fpm
master (ไม่เรียก `EnsureUser`/`GrantGroupAccess`/`RemoveUser` เลยบน
macOS) ส่วน Linux ยังคงเรียก flow เดิมทุกอย่าง (`internal/cliapp/
service.go` ฟังก์ชัน `setupPHPPool`/`teardownPHPPool` แยก branch ตาม
`osutil.IsMacOS()`)

**ข้อควรรู้**: การแก้นี้ใช้ได้เฉพาะ pool ที่ถูกสร้าง/แก้ไข**หลังจาก**การ
แก้นี้ deploy แล้วเท่านั้น (ตาม pattern เดิมของฟีเจอร์นี้ที่ไม่มี
migration บังคับ) php service ที่สร้าง pool ไว้บน macOS ก่อนการแก้นี้
จะยังมีไฟล์ `.conf` และ unix user เฉพาะตัวแบบเก่าค้างอยู่ ไม่ self-heal
เอง ต้อง `wor service remove` แล้ว `wor service add` ใหม่ (สำรอง source
ก่อนด้วย `wor source backup` เพราะ `service remove` จะลบ directory
ของ service ทิ้งทั้งหมด) ยังไม่มีคำสั่ง "ซ่อม pool ที่มีอยู่แล้วในที่"
แบบเบา ๆ ให้ใช้

นอกจากนี้ยังพบว่าการเดาชื่อ Homebrew formula ว่าเป็น `php@<version>`
เสมอนั้นผิดได้เช่นกัน -- บางเครื่องติดตั้ง PHP ผ่าน formula `php` เฉย ๆ
(ไม่ตั้งชื่อเวอร์ชัน) โดยที่เวอร์ชันนั้นบังเอิญเป็นเวอร์ชันล่าสุด ไม่มี
`php@X.Y` keg แยกต่างหากเลย ทำให้การเดา path ของ binary และชื่อ service
ผิดไปด้วย (`internal/phpfpm` เดิม hardcode `ReloadUnit: "php@" + version`
เสมอ) แก้โดยเพิ่ม `resolveHomebrewPHPBinary` ที่ลอง path แบบ versioned
ก่อน แล้ว fallback ไปเช็ค formula `php` เฉย ๆ เฉพาะตอนที่ binary นั้น
ยืนยันเวอร์ชันตรงกับที่ต้องการจริง ๆ เท่านั้น (เช็คผ่าน `<binary> -v`
ไม่เดาเอาเฉย ๆ เพื่อไม่ให้เครื่องที่มีหลายเวอร์ชัน PHP ติดตั้งพร้อมกัน
จับผิดเวอร์ชันกัน)

## 9. `wor run`: ทำให้ทุก service ที่ enabled ทำงานอยู่ (ใหม่)

คำสั่งใหม่ที่ตรวจสอบและสตาร์ทบริการทุกตัวที่ enabled ไว้ทั้งเครื่อง
พร้อม runtime/web server ที่จำเป็น ตั้งใจตั้งชื่อว่า `run` แทนที่จะเป็น
`start`/`up` เพราะเป็นคำสั่งทิศทางเดียว -- "ทำให้ระบบอยู่ในสถานะที่
ต้องการ" คล้าย `terraform apply`/`docker-compose up` ไม่มีคำสั่งคู่แบบ
`wor down`/`wor stop-all` ตามมา (design ตกลงกันไว้ก่อนเขียนโค้ดผ่านการ
คุยหลายรอบ)

ลำดับการทำงาน:
1. เช็คทีเดียวก่อนเข้า loop ต่อ service: web server provider ที่ active
   อยู่ (start ให้ถ้ายังไม่ทำงาน -- เพิ่ม `Provider.IsRunning()`/
   `Provider.Start()` ใหม่ใน `internal/hostprovider` เพราะเดิมมีแค่
   `Reload()` ที่สมมติว่า server ทำงานอยู่แล้วเสมอ) และ pm2 daemon
   (เฉพาะถ้ามี service ที่ต้องใช้ pm2 จริง)
2. **ปิดช่องโหว่ pm2 boot persistence**: ถ้า `pm2 startup` ไม่เคยถูก
   ลงทะเบียนบนเครื่องนี้เลย (ไม่มีอะไรใน wor เคยเรียกมันมาก่อน ทำให้
   service ที่ใช้ pm2 ไม่กลับมาทำงานเองหลัง reboot) จะเสนอลงทะเบียนให้
   ทันที โดยขอ `pm2 startup` เองก่อนเพื่อเอาคำสั่งที่มันแนะนำมา (pm2 ไม่
   apply อะไรเอง แค่ print คำสั่ง `sudo ...` ให้ไปรันเอง) แสดงคำสั่งเต็ม
   ให้ user เห็นก่อนเสมอ แล้วรันผ่าน `osutil.SudoCommand` (confirm-once
   elevation gate เดียวกับที่อื่นในโปรเจกต์ ไม่ใช่แค่ print ให้ copy-paste
   เอง)
3. วน loop ทีละ service ที่ enabled: เช็ค/สตาร์ท runtime ที่ต้องใช้ก่อน
   (สำหรับ php ที่มี pool เฉพาะตัว -- เพิ่ม `phpfpm.IsRunning()`/
   `phpfpm.Start()` ใหม่ด้วยเหตุผลเดียวกับ web server provider) แล้วค่อย
   สตาร์ท service เอง ถ้ายังไม่ทำงาน (pm2/systemd ใช้ path เดียวกับที่
   `wor service start` ใช้อยู่แล้ว)

service ที่ล้มเหลวจะถูกข้ามไป ไม่ทำให้คำสั่งทั้งหมดหยุด แสดงผลทีละ
service เป็น ok/fail ระหว่างทาง จบด้วยสรุปบรรทัดเดียวว่าสำเร็จ/ล้มเหลวกี่
service

### บันทึกจากการทดสอบจริง (ต้องพึ่ง output จริงถึงจะวินิจฉัยถูก)

หลายจุดของ `wor run` วินิจฉัยไม่ถูกจนกว่าจะเห็น output จริงจากเครื่อง
user เท่านั้น เป็นบทเรียนสำคัญว่าฟีเจอร์ที่พึ่งพา behavior ของเครื่องมือ
ภายนอก (pm2, Homebrew, launchd) verify ด้วยการอ่านโค้ดอย่างเดียวไม่พอ:

- **platform keyword ของ `pm2 startup`**: เดาผิดว่า macOS ใช้คำว่า
  `launchd` (จริง ๆ ไม่ใช่ keyword ที่ pm2 รู้จัก) แก้โดยไม่ส่ง platform
  argument เลย ปล่อยให้ pm2 auto-detect เอง
- **exit code ของ `pm2 startup` ไม่ใช่ signal ความสำเร็จที่เชื่อถือได้**:
  แม้ตอนที่ pm2 ทำงานสำเร็จปกติ (detect platform ได้ print คำสั่งแนะนำ
  ออกมาถูกต้อง) exit code ก็ยังไม่ใช่ 0 แก้โดยเช็คจากเนื้อหา output ว่ามี
  บรรทัด `sudo ...` หรือไม่ แทนการเช็ค exit code
- **`$PATH` ไม่ขยายค่าถ้าไม่ผ่าน shell จริง**: คำสั่งที่ pm2 แนะนำมี
  `env PATH=$PATH:/usr/local/bin ...` ซึ่งต้องให้ shell ขยายค่า `$PATH`
  ก่อนที่ `env`/`sudo` จะเห็น ถ้า exec คำสั่งตรง ๆ (แยกเป็น argv เอง)
  `$PATH` จะไม่ถูกขยาย กลายเป็น string ดิบที่มีเครื่องหมาย `$` ติดไปด้วย
  ทำให้ PATH ที่ตั้งจริงพัง (`mkdir` หาไม่เจอ) แก้โดยรันทั้งบรรทัดผ่าน
  `sh -c` แทนการ parse เป็น argv เอง

## 10. รีดีไซน์ `wor service status` และ `wor host list`

`service status` เดิมแค่เรียก `pm2 status` ตรง ๆ ทำให้เห็นแค่ node
service เท่านั้น go/python (ที่ใช้ systemd บน Linux) กับ php/static
service มองไม่เห็นเลย ตอนนี้จะรวบรวม service ที่ enabled ทุกตัวจากทุก
domain (`Store.ListAllServices`) แล้วจัดกลุ่มตาม process provider จริง
(`domainmodel.ProcessProviderFor`) query สถานะจริงของแต่ละ provider:
`pm2 jlist` ครั้งเดียวสำหรับ node service ทั้งหมด บวกกับการ sample
`systemctl` แบบ batch ครั้งเดียว (`systemd.GetInfoBatch`) สำหรับ go/python
ทั้งหมด เพื่อให้ค่าใช้จ่ายในการ query ของ pm2/systemd เกิดขึ้นแค่ครั้งเดียว
ไม่ว่าจะมีกี่ service php (สมมติว่า php-fpm ทำงานอยู่แล้ว) กับ static
(ไม่มี process) ไม่มีอะไรให้ query เลยจึงแสดงสถานะ n/a แทนที่จะถูกซ่อน
ไปเฉย ๆ

`host list` เดิมแค่ dump รายชื่อไฟล์ `.conf` ใน sites-available ตอนนี้
เทียบ sites-available กับ sites-enabled เพื่อแบ่งกลุ่ม ENABLED/DISABLED
พร้อมแสดง target ที่ resolve แล้ว (`domain/service`), port, และ badge SSL
ของแต่ละ site ด้วย

ทั้งสองคำสั่ง render ผ่าน helper ร่วมกันใน `internal/cliapp/statusview.go`:
ใช้สี ANSI เมื่อเป็น terminal จริง, ใช้ tag แบบวงเล็บธรรมดา
(`[ok]`/`[fail]`/`[on]`/`[off]`/`[ssl]`) เมื่อไม่ใช่ (ปิดสีได้ผ่าน env
`NO_COLOR`) ไม่ใช้ library สีจากภายนอกเลย (โปรเจกต์นี้ตั้งเป้าไม่มี
third-party dependency)

## 11. รีดีไซน์ `wor doctor`

จากรูปแบบเดิมที่ยาวและมีหัวข้อ Environment/Directories/Required-Optional
-Dependencies/Result/"WOR Ready"/"Next" เปลี่ยนเป็น checklist แบบ
✓/⚠/✗ เรียบ ๆ จัดกลุ่มเป็น Environment (ย่อเหลือแค่ OS/WOR_ENV/WOR_HOME/
Config/Host Provider + บรรทัดเดียวสรุปว่า workspace initialize แล้วหรือ
ยัง), Runtimes, Database, Other Tools -- ไม่มีส่วน "Result" ปิดท้ายอีก
ต่อไป

PHP/Node.js/Python/Go จะเป็น ✗ ทันทีถ้าไม่ได้ติดตั้ง (ตัดเงื่อนไขเดิมที่
เช็คว่า "มี service ที่ต้องใช้ runtime นี้จริงไหม" ออกไปทั้งหมด) Nginx
กับ Apache แสดงทั้งคู่ถ้าต่างก็ติดตั้งอยู่ (ติด label "(active)" ให้ตัวที่
ตรงกับ HOST_PROVIDER) และจะเป็น ✗ เฉพาะถ้าตัวที่ *active* หายไป (host
provider ไม่ตรงกับที่ติดตั้งจริง) -- ตัวที่ไม่ได้ active ขาดหายไปไม่ถือ
เป็นปัญหา ฐานข้อมูล (MySQL Client/Server, MariaDB, PostgreSQL, Redis,
SQLite) กับเครื่องมืออื่น (git/zip/gzip) เป็น optional เสมอ ขาดไปแค่ ⚠
ไม่ใช่ ✗

## 12. รีดีไซน์การยืนยันของ `wor domain remove`

`domain remove` **ไม่มี** flag `--cascade`/force ใด ๆ เลย -- บล็อกทันที
ถ้า `services.config.json` ของ domain ยังมี service อยู่แม้แต่ตัวเดียว
(ต่อให้หยุดทำงานแล้วก็ตาม) แสดง service ที่ค้างพร้อมคำสั่งแก้ที่ชัดเจน
(`wor service remove <domain>/<service>`) เพราะ "domain" ในความหมายของ
wor คือแค่โฟลเดอร์ config/source เท่านั้น ไม่ครอบคลุมถึง process
pm2/systemd หรือ host config ของ service เลย -- ต้องเคลียร์ผ่าน
`service remove` ก่อน (ซึ่งจัดการความสะอาดส่วนนั้นอยู่แล้ว)

เมื่อไม่มี service เหลือแล้ว จะถามทีละขั้นตอนแบบ `[Y/n]` (default yes)
เรียงลำดับ **Backups -> Logs -> Web Data**: Backups/Logs แค่ถูก
"บันทึกการตัดสินใจ" ไว้ (พร้อม preview ทันทีว่าจะลบหรือเก็บ) ยังไม่มี
อะไรถูกลบจริง **Web Data ที่ถามเป็นคำถามสุดท้ายคือจุดยืนยันของทั้งชุด**:
ตอบ "n" จะยกเลิกทั้งหมด (Backups/Logs ที่เลือกไว้ก่อนหน้าจะถูกทิ้งไปเฉย ๆ
ไม่ถูกลบ) ตอบ "y" จะรันทั้งสามอย่างตามที่เลือกไว้ในคราวเดียว (Backups
ก่อน แล้ว Logs แล้วค่อย Web Data เอง)

## 13. `wor source backup` กรองไฟล์ผ่าน `.gitignore`

ค่าเริ่มต้น (เปิดไว้) จะกรองไฟล์ที่ zip ผ่าน `.gitignore` ของ source
tree เองด้วย ไม่ใช่แค่ exclude list ที่ตั้งไว้ใน `backup.config.json`
เท่านั้น package ใหม่ `internal/gitignore` (ไม่มี dependency ภายนอก
ตาม policy ของโปรเจกต์) เป็น matcher ที่ **ตั้งใจอ่านแค่ `.gitignore`
ไฟล์เดียวที่ root** ของ directory ที่กำลัง zip ไม่รองรับ `.gitignore`
ซ้อนในแต่ละ subfolder แบบที่ git จริงทำ (trade-off ที่เลือกไว้เพื่อไม่
ต้องเขียน matcher เต็มรูปแบบที่ซับซ้อนกว่านี้มาก) รองรับ comment,
บรรทัดว่าง, negation ด้วย `!`, การ anchor ด้วย `/` นำหน้า/กลาง,
directory-only ด้วย `/` ท้าย, และ wildcard `*`/`?`/`[...]`/`**` --
กฎล่าสุดที่ match ชนะเหมือน git จริง `wor source backup <target>
--gitignore=enable|disable` override ค่า default นี้ได้แค่ครั้งเดียว
โดยไม่แก้ config

## 14. `wor source clone` ไม่ต้องใส่ `--replace` อีกต่อไป

ถ้า target มี source อยู่แล้ว จะสำรอง (ผ่าน `wor source backup`) แล้ว
แทนที่ให้อัตโนมัติเสมอ ไม่ต้องใส่ flag ใด ๆ เพิ่ม (`--replace` ถูกตัด
ออกจาก usage แล้ว ถ้ามี script เก่าที่ยังส่ง flag นี้มาก็แค่ถูกเพิกเฉย
ไม่ error) การแทนที่จะย้าย tree เดิมไปพักไว้ก่อนเสมอ (ไม่ลบทิ้งตรง ๆ)
แล้วค่อยทิ้งของเก่าจริง ๆ ก็ต่อเมื่อย้าย tree ใหม่เข้าที่สำเร็จแล้ว
เท่านั้น ถ้าย้ายไม่สำเร็จจะย้ายของเก่ากลับมาที่เดิมให้ (rollback)

การย้าย directory (`moveDir`) จะลอง `os.Rename` ก่อน (เร็วกว่า) แล้ว
fallback ไปเป็น copy+remove ถ้า rename ล้มเหลวแบบ "invalid cross-device
link" (เกิดได้เมื่อ tmp directory ที่ configure ไว้กับ WOR_HOME อยู่คนละ
filesystem กัน) ไม่ได้พยายามตรวจ errno เฉพาะแบบข้าม Linux/macOS/Windows
-- rename ล้มเหลวแบบไหนก็ fallback ไป copy เหมือนกันหมด

## 15. `wor database add`/`remove`: เข้มงวดกับ validation มากขึ้น

`add` ไม่สร้าง domain ให้อัตโนมัติอีกต่อไป -- error ทันทีว่า "domain not
found" ถ้า `WOR_HOME/domains/<domain>` ยังไม่มีอยู่จริง โปรไฟล์ที่ซ้ำกัน
(มีอยู่แล้วใน `databases.config.json`) ไม่ error แต่พิมพ์ `[WARN]`
เตือนแทน (ไม่แตะ label/.env เดิม) `remove` error ถ้า domain ไม่มีอยู่จริง
และ error ถ้าโปรไฟล์ไม่ได้ลงทะเบียนไว้ (เดิมเป็น no-op เงียบ ๆ ทั้งสอง
กรณี) และแก้บั๊กจริงที่พบ: `remove` เดิมไม่เคยลบไฟล์ `<profile>.env`
ใต้ `configs/database/` เลย ลบแค่ entry ใน config ตอนนี้ลบไฟล์ `.env`
ด้วย (ถ้าไฟล์หายไปแล้วก็แค่เตือน ไม่ error)

## Known gaps / สิ่งที่ยังต้อง verify

- **Build/run จริงแล้วบางส่วน**: ตอน port ครั้งแรก sandbox ที่ใช้เขียน
  ไม่มี Go toolchain เลย โค้ดตอนนั้นไม่เคยถูก compile หลังจากนั้น user ได้
  `go build`/รันจริงบนเครื่อง macOS ของตัวเองแล้ว (`./scripts/build.sh`)
  พบและแก้บั๊กจริงหลายจุดที่อ่านโค้ดอย่างเดียวมองไม่เห็น (ดูหัวข้อ 8/9
  ด้านบน) แต่ยังไม่ได้ทดสอบครบทุก path บนเครื่องจริง โดยเฉพาะ:
  - `wor run`'s pm2-startup registration flow ผ่านการแก้ไข 3 รอบแล้ว
    (platform keyword ผิด -> เช็ค exit code ผิด -> `$PATH` ไม่ขยายค่า)
    รอบล่าสุดยังไม่ได้รับการยืนยันจาก user ว่าทำงานสำเร็จจริง
  - Per-service php-fpm pool: ยืนยันแล้วว่าใช้งานได้จริงบน macOS หลัง
    แก้เรื่อง unix user (หัวข้อ 8) แต่ยังไม่เคยทดสอบบน Linux จริงเลย
    (ทั้ง `useradd`, `php-fpm -t`, `systemctl reload`)
- Path default ของ nginx/apache บน Windows (หัวข้อ 3) เป็นแค่ convention
  ที่เดาไว้ ไม่เคย verify กับ nginx/Apache ตัวจริงบน Windows เลย --
  คาดว่าต้อง override ผ่าน `host.env` อย่างน้อยหนึ่งครั้ง
- PM2 บน Windows: ตัว PM2 เองเป็น npm package ควรทำงานได้ แต่ยังไม่เคย
  ถูกทดสอบเป็นส่วนหนึ่งของการพอร์ตนี้เลย
- flag แบบ single-dash สั้น ๆ ที่ shell เวอร์ชันเดิมรับ (เช่น `-y` คู่กับ
  `--yes`) ตัว flag parser ของ Go (`internal/cliapp/args.go`) ไม่รองรับ
  -- ใช้ได้แค่ฟอร์มยาวเท่านั้น เพิ่มได้ไม่ยากถ้ามี script ที่พึ่งฟอร์มสั้น
  อยู่
- Linux สาย RHEL ใช้โครงสร้าง php-fpm package ต่างจาก
  `/etc/php/<version>/fpm` และยังไม่รองรับ auto-detect
  (`phpfpm.DetectVersions`)
- php service ที่สร้าง per-service pool ไว้บน macOS **ก่อน**การแก้ไข
  เรื่อง unix user (หัวข้อ 8) จะยังมี pool `.conf`/unix user แบบเก่าค้าง
  อยู่ ไม่ self-heal เอง ต้อง remove+add ใหม่ด้วยมือ ยังไม่มีคำสั่ง
  "ซ่อม pool ที่มีอยู่ในที่" แบบเบา ๆ ให้ใช้
