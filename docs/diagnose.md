# wor diagnose -- Design Doc (ร่าง ยังไม่ implement)

สถานะ: implement แล้ว (2026-07-06) ที่ `internal/cliapp/diagnose.go`
(+ ส่วนเพิ่มเล็ก ๆ ใน internal/pm2 และ internal/systemd)

ปรับรอบสอง (2026-07-06, หลังทดสอบบนเครื่องจริง): แก้ false positive
3 จุด (DocumentRoot แบบ relative, `nginx -t` ที่ [emerg] เพราะ
permission, stat ไฟล์ cert ใน /etc/letsencrypt ที่ root-only) และ
เปลี่ยน verdict เป็นแบบ ranked -- ดูหัวข้อ "Verdict" ด้านล่างซึ่ง
อัปเดตตามแล้ว

## เป้าหมาย

เมื่อ service ล่ม admin ต้องรู้ **สาเหตุ** และ **คำสั่งแก้** ภายในการรัน
คำสั่งเดียว เป้าหมายคือลด MTTR (เวลากลับมา online) ไม่ใช่รายงานสถานะ
สวย ๆ -- `wor info` ทำหน้าที่นั้นอยู่แล้ว

การแบ่งงานกับคำสั่งเดิม:

- `wor doctor` -- สุขภาพทั้งเครื่อง (runtime ครบไหม ติดตั้งถูกไหม)
- `wor info` -- แสดงสถานะ service หนึ่งตัว (ไม่ตัดสินอะไร)
- `wor health` -- กวาดหา service ที่พังทั้งเครื่อง (end-to-end)
- `wor diagnose` -- หา root cause ของ service ที่มีปัญหา + แนะนำวิธีแก้
- `wor run` -- คำสั่งกู้คืน (ensure ทุกอย่างที่ enabled ขึ้นครบ)

workflow ตอน server ล่ม: health หาตัวพัง -> diagnose บอกสาเหตุ ->
run เอากลับมา

## รูปแบบคำสั่ง

    wor diagnose <host|domain/service>
    wor health

- target แบบเดียวกับ `wor info`: host เดี่ยว resolve ผ่าน
  `Store.ResolveHost`, หรือ `domain/service` ตรง ๆ
- `wor health` (เดิมคือ `wor diagnose --all`; แยกเป็นคำสั่งของตัวเอง
  2026-07-06 -- "diagnose" คือวิเคราะห์ผู้ป่วยที่รู้ตัวแล้ว ส่วนนี่คือ
  การ*หา*ว่าใครป่วย) ไล่ตรวจทุก service ที่ enabled แบบย่อ: ชั้น
  process/port ต่อตัว + **HTTP จริงผ่าน web server หนึ่ง request ต่อ
  service** (host แรกที่ลงทะเบียน แสดงผลเป็นบรรทัดย่อยใต้แต่ละ
  service) แล้วสรุปเฉพาะตัวที่มีปัญหา -- สำหรับกรณี "เว็บล่มแต่ยัง
  ไม่รู้ตัวไหน" เช่นหลังเครื่อง reboot
  ส่วน http จำเป็น ไม่ใช่ของแถม: ปัญหา permission/vhost/proxy
  ไม่เคยฆ่า process (เหตุการณ์จริง 2026-07-06: pool "accepting"
  ทั้งที่ nginx ตอบ 403/502) ตีความ 2xx/3xx = ok, **404 = ok พร้อม
  หมายเหตุ** (API จำนวนมากไม่มีหน้า `/` -- FAIL ปลอมจะทำให้เลิก
  เชื่อคำสั่ง), 4xx/5xx/refused/timeout = FAIL
  เส้นแบ่งกับ `wor doctor`: doctor = เครื่อง/runtime พร้อมไหม,
  health = service ยังเสิร์ฟได้ไหม
- read-only ทั้งหมด **ไม่ auto-fix** (สอดคล้อง Safety Rules ใน
  AGENTS.md) -- แสดงคำสั่งแก้ให้ copy-paste เท่านั้น
- exit code: 0 = ไม่พบปัญหา, 1 = พบปัญหา (ใช้กับ cron/monitoring ได้)
- ทุก probe มี timeout (HTTP 5 วินาที) -- ทั้งคำสั่งต้องจบใน ~10-15
  วินาที เพราะตอนไฟไหม้ห้ามให้เครื่องมือวินิจฉัยช้าเสียเอง

## หลักการวินิจฉัย: ไล่ตาม request path

ตรวจเป็นชั้นจากนอกเข้าใน **failure จุดแรกในสายคือ root cause
ที่น่าจะเป็นที่สุด** ชั้นถัดไปที่ตรวจไม่ได้เพราะชั้นก่อนพังให้ขึ้น SKIP

### ชั้น 1: Config

- service มีใน services.config.json, enabled, template อะไร
- entry point มีจริงบน disk (node/go/python) และ executable (go)
- FAIL ที่พบบ่อย: deploy ไม่ครบ, binary ยังไม่ build

### ชั้น 2: DNS / hosts

- host resolve ได้ไหม (`net.LookupHost`) และชี้มาเครื่องนี้จริงไหม
  (เทียบกับ IP ของเครื่อง / 127.0.0.1 สำหรับ local domain)
- local domain: มี entry ใน /etc/hosts ตามที่ลงทะเบียนไหม

### ชั้น 3: Web server

- nginx/apache ตัวแม่รันอยู่ไหม (process + `systemctl is-active`
  บน Linux)
- vhost config ของ host นี้มีอยู่และ enabled
- `nginx -t` / `apachectl configtest` ผ่านไหม (config เสียตัวเดียว
  ทำให้ reload ไม่ได้ทั้งเครื่อง)
- SSL: ไฟล์ cert/key มีจริง, **วันหมดอายุ** (แจ้งจำนวนวันที่เหลือ,
  FAIL ถ้าหมดแล้ว, WARN ถ้า < 14 วัน)

### ชั้น 4: Process (แยกตาม provider เดียวกับ `wor info`)

pm2 (node ทุก OS, go/python บน macOS):

- pm2 daemon เองรันอยู่ไหม -- ถ้า daemon ว่าง/ตาย และ uptime เครื่อง
  ต่ำ ให้ verdict ตรง ๆ ว่า "เครื่องเพิ่ง reboot และ pm2 ไม่มี boot
  persistence -- รัน `wor run`" (จุดอ่อนที่รู้อยู่แล้วของระบบ)
- สถานะ process: errored / stopped / online
- restart count + uptime: restart สูงใน 10 นาที = crash loop,
  uptime ไม่กี่วินาที = เพิ่งตายซ้ำ ๆ

systemd (go/python บน Linux):

- `systemctl show <unit>` อ่าน `ActiveState`, `SubState`, `Result`
  (exit-code / oom-kill / signal / start-limit-hit), `NRestarts`,
  `ExecMainStatus`
- `Result=oom-kill` ให้ verdict เรื่อง memory ทันที
- `start-limit-hit` = crash loop ฝั่ง systemd

php-fpm (php ที่มี dedicated pool):

- master process ของเวอร์ชันนั้นรันไหม, pool file มีอยู่ไหม,
  socket มีจริง + web user เข้าถึงได้
- `php-fpmX.Y -t` ผ่านไหม (ใช้ sudo บน Linux ตามที่แก้ไว้แล้ว)
- pool แบบ legacy (PHPVersion ว่าง): เช็คแค่ endpoint ใน config
  ตอบสนองไหม

static: ข้ามชั้นนี้ (ไม่มี process)

### ชั้น 5: Port / Socket

- มี process ฟังบน `svc.Port` ไหม (อ่าน /proc/net/tcp บน Linux หรือ
  `lsof -i` fallback) และ **PID ตรงกับ process ของ service นี้ไหม**
- port ถูก process อื่นแย่ง = root cause คลาสสิก (EADDRINUSE) --
  แสดงชื่อ/PID ตัวที่แย่งให้เลย

### ชั้น 6: HTTP probe สองชั้น (ตัวชี้ขาด)

1. ยิงตรง `http://127.0.0.1:<port>` (ข้าม web server)
2. ยิงผ่าน host จริง (ตาม SSL state)

ตีความ:

| ตรง | ผ่าน host | สรุป |
|-----|-----------|------|
| OK | OK | service ปกติ (ปัญหาอาจอยู่นอกเครื่อง เช่น DNS จริง/firewall) |
| OK | FAIL | ปัญหาที่ web server / vhost / SSL |
| FAIL | - | app ตายเอง -> ดูชั้น 4 + logs |

status code ผ่าน host: 502 = upstream ตาย, 504 = app ค้าง/ช้า,
403 = permission (โยงชั้น 7), 404 = docroot/route ผิด

php/static ไม่มี port -- ยิงผ่าน host อย่างเดียว

### ชั้น 7: Filesystem / Permissions

- reachability check ตัวเดียวกับ `wor info` / `wor doctor` Security
  (web user traverse ถึง docroot ได้ไหม) -- reuse โค้ดเดิมทั้งก้อน
- disk เต็ม: ถ้า usage ของ filesystem ที่ WOR_HOME อยู่ >= 95% ให้
  WARN แรง ๆ (สาเหตุเงียบที่ทำให้ log/db เขียนไม่ได้)

### ชั้น 8: Logs -- ดึงมาให้ ไม่ต้องไปหาเอง

แหล่ง log ตาม provider: pm2 error log, `journalctl -u <unit> -n 30`,
php-fpm error log ของ pool, nginx/apache error log (filter ตาม host
เท่าที่ทำได้) เอา 20-30 บรรทัดท้าย

จับ pattern ที่รู้จักแล้วแปลเป็นสาเหตุ + วิธีแก้:

| pattern | สาเหตุ | แนะนำ |
|---------|--------|-------|
| `EADDRINUSE` | port ชน | หาตัวแย่ง port / เปลี่ยน port |
| `MODULE_NOT_FOUND` / `Cannot find module` | ไม่ได้ npm install | `npm install` ใน service dir |
| `permission denied` | สิทธิ์ไฟล์/socket | คำสั่ง setfacl/chmod ตามบริบท |
| `Out of memory` / oom-kill | RAM ไม่พอ | เช็ค memory / ลด worker |
| `ENOENT` | ไฟล์/path หาย | เช็ค entry point, .env |
| `SSL_ERROR` / `certificate` | cert มีปัญหา | `wor ssl ...` |

ตารางนี้เก็บเป็น slice ใน Go ธรรมดา เพิ่ม pattern ทีหลังง่าย
(ไม่ต้องมีระบบ plugin -- Simplicity First)

## Verdict: ส่วนที่สำคัญที่สุดของ output

หลักการแกนกลาง (ยืนยันโดย Project Owner): **"หนึ่งปัญหาหลัก (Root
Cause) + หลักฐาน + วิธีแก้"** -- ผู้ใช้ต้องไม่ต้องตีความเองจากรายการ
FAIL หลายอัน เพราะการสังเคราะห์นี้คือคุณค่าที่ทำให้ wor diagnose
ต่างจากการรัน systemctl/pm2/nginx -t/curl แยกกันเอง

กลไก: ทุก FAIL สร้าง "cause" ที่มี kind (ตระกูลปัญหา: proc, perm,
port, tls, ...) + confidence 3 ระดับแบบ static (high/medium/low)

- cause ที่ kind เดียวกันจาก**คนละชั้น** merge เป็นอันเดียว (เช่น
  http 403 + files-blocked = ปัญหา permission เดียวกัน) โดย
  confidence สูงกว่าเป็นผู้ชนะทั้งความมั่นใจและถ้อยคำ -- นี่คือ
  cross-layer boost ของคู่ files+http ที่ตกลงกัน
- log pattern ที่ match จะ**ยืนยัน** cause เดิม (ดัน confidence เป็น
  high) หรือ**เติมเหตุผล**เข้า cause ฝั่ง process ("app crashes on
  start -- Cannot find module") แทนที่จะโผล่เป็นรายการแยก
- pattern เดี่ยว ๆ ที่ไม่มีชั้นไหนยืนยัน = confidence low

จัดอันดับด้วย confidence ก่อน แล้ว tie-break ด้วยลำดับชั้นตาม
request path

Layout สุดท้าย (ตกลง 2026-07-06): **Checks (พิมพ์สดระหว่างเช็ค) ->
Summary -> Evidence -> Suggested fix** -- Summary อยู่หลัง Checks
โดยตั้งใจ เพราะ check rows พิมพ์สดทีละบรรทัดเพื่อให้เห็น progress
(ตอนทุกอย่างล่ม probe รวมกันกินเวลาได้ ~10-15 วิ จอว่างเปล่าแย่กว่า)
และ verdict อยู่ท้ายสุดติด prompt ซึ่งเป็นจุดที่ตาเห็นก่อน

1. **Summary** -- `Status: FAILED (N fail, M warn)` + **Root cause
   หนึ่งเดียว** + **Cascade**: รายการ FAIL อื่นที่*พิสูจน์แล้ว*ว่า
   เป็นปัญหาเดียวกัน (มาจาก kind merge เท่านั้น ไม่เดา causal link
   เด็ดขาด -- อ้างผิดแย่กว่าไม่อ้าง)
2. **Evidence** -- จัดกลุ่มตาม source, ยุบบรรทัดซ้ำเป็น "(xN)"
   (เทียบแบบตัดตัวเลขทิ้ง: timestamp/pid/connection id ต่างกันทุก
   ครั้งที่ log ซ้ำ), ตัดที่ ~160 ตัวอักษร, สูงสุด 3 บรรทัด/source
3. **Suggested fix** -- runbook มีเลขลำดับ: ข้อ 1 แก้ root cause,
   ข้อถัดไปเป็น cause อิสระที่เหลือตาม rank ("If it persists -- ..."),
   ปิดท้ายด้วยขั้น Verify (`wor diagnose <target>` ซ้ำ) --
   หลักคือแก้เหตุก่อน verify ทีหลังเสมอ
4. **Also worth checking** -- คำแนะนำหลวม ๆ (cert ใกล้หมดอายุ,
   process flapping, rollback hint)

กรณีพิเศษที่ควร detect ตรง ๆ เพราะเจอบ่อยและกู้เร็ว:

- เครื่องเพิ่ง reboot + pm2 ว่าง -> `wor run`
- service ตายหลัง deploy ล่าสุดไม่นาน (เทียบเวลา process ตายกับ git
  commit / deploy ล่าสุด) -> เสนอ `wor rollback <target>`
- cert หมดอายุ -> ชี้คำสั่ง ssl renew
- ทุกชั้นผ่านหมด -> บอกตรง ๆ ว่า "ในเครื่องปกติ ปัญหาน่าจะอยู่ภายนอก
  (DNS จริง, firewall, CDN)" ดีกว่าเงียบ

## ตัวอย่าง output

    $ wor diagnose api.example.com

    WOR Diagnose
    ------------
    Target : example.com/api
    Host   : api.example.com  [ssl: letsencrypt]
    Runtime: node v20.11.1
    Server : nginx (nginx/1.24.0)

    Checks
    ------------------------------------------------
    [PASS] config    enabled (node, port 3100), entry app.js found
    [PASS] dns       api.example.com -> 127.0.0.1 (this machine)
    [PASS] nginx     running, vhost ok, config test ok
    [PASS] ssl       letsencrypt cert valid (45d left)
    [FAIL] process   pm2 status: errored (15 restarts)
    [SKIP] port      (process not running)
    [FAIL] http-app  direct 127.0.0.1:3100 -> connection refused
    [FAIL] http-host via api.example.com :443 -> 502
    [WARN] logs      1 known error pattern(s) in pm2 error log -- see evidence below

    Summary
    ------------------------------------------------
    Status: FAILED (3 fail, 1 warn)

    Root cause:
      app crashes on start (pm2 gave up restarting it) -- Node.js dependency missing (Cannot find module)

    Cascade (same problem, seen from other layers):
      - http-app: direct 127.0.0.1:3100 -> connection refused
      - http-host: via api.example.com :443 -> 502

    Evidence
    ------------------------------------------------
      pm2 error log:
        Error: Cannot find module 'express'

    Suggested fix (run yourself -- wor diagnose never changes anything)
    ------------------------------------------------
    1. Fix the root cause -- app crashes on start (...) -- Node.js dependency missing:
         wor service logs example.com/api
         wor service restart example.com/api
         cd <WOR_HOME>/domains/example.com/api && npm install && wor service restart example.com/api
    2. Verify:
         wor diagnose example.com/api

    exit status 1

สังเกตว่า FAIL สามชั้น (process, http-app, http-host) ถูกสังเคราะห์
เหลือ Root cause เดียว: http-app/http-host เป็น kind "proc"
เหมือนกันจึง merge เข้า cause ของชั้น process (แล้วโผล่ใน Cascade
พร้อมระบุว่ามาจาก layer ไหน) และ log pattern เติมเหตุผล
"Cannot find module" เข้าไปในถ้อยคำของ root cause

## แนวทาง implement (สรุปผลกระทบ)

- ไฟล์ใหม่ `internal/cliapp/diagnose.go` + subcommand ใน `app.go`/
  `usage.go` -- ไม่แตะ workflow เดิม
- reuse ของเดิมเกือบทั้งหมด: target resolve จาก `info.go`, process
  status จาก `pm2`/`systemd`/`phpfpm` packages, reachability จาก
  `doctor.go`, SSL state จาก `ssl.LoadState`
- ของใหม่จริง ๆ มีแค่: HTTP probe (net/http ธรรมดา), port-listener
  lookup, cert expiry parse (crypto/x509), log pattern table --
  ทั้งหมดใช้ Go standard library (ตาม Dependency Policy)
- โครงสร้างภายใน: check แต่ละตัวเป็น func คืน
  `{status, label, detail, fix}` รันเรียงตามชั้น เก็บผลลง slice
  แล้วค่อย render + สรุป verdict ตอนท้าย -- เพิ่ม check ใหม่ =
  เพิ่ม func เดียว
- cross platform: Linux ครบทุกชั้น, macOS ข้าม systemd/reachability
  ตามเงื่อนไขเดิมของ info/doctor, Windows ตรวจเท่าที่ตรวจได้
  (config/process/port/http) และแจ้งชัดว่าข้ามอะไร

## นอกขอบเขต (ตัดออกโดยตั้งใจ)

- auto-fix / `--fix` ที่ลงมือเอง -- เสี่ยงเกินไปตอน production ล่ม
- watch mode / monitoring ต่อเนื่อง -- ใช้ cron + exit code แทน
- `--json` -- ค่อยเพิ่มทีหลังถ้ามีระบบ monitoring มาต่อ ไม่บล็อก v1
- การวิเคราะห์ performance (ช้าแต่ไม่ล่ม) -- คนละโจทย์กับ "ล่ม"
