Service Runtime Template

หมายเหตุข้าม platform: systemd มีเฉพาะบน Linux เท่านั้น บน macOS และ Windows
เทมเพลต `go` และ `python` ด้านล่างจะ fallback ไปใช้ PM2 (ตัวเดียวกับที่
`node` ใช้เสมอ) แทน systemd เพื่อไม่ให้ทั้งสอง platform ขาดวิธีรันบริการ
ไปเลย -- ดู DESIGN.md หัวข้อ 6 ประกอบ คำสั่ง `wor doctor` จะรายงานว่า
process provider ตัวไหนกำลัง active อยู่บนเครื่องปัจจุบัน

- static
    Runtime: ไม่มี
    Process Provider: ไม่มี
    Web Server: เสิร์ฟโฟลเดอร์ public/ ตรง ๆ

- node.js
    Runtime: Node.js
    Process Provider: PM2
    Entry Point: app.js (ค่าเริ่มต้น)
    ปรับแต่งได้: ได้
    ตรวจสอบ Runtime:
      - แสดงเวอร์ชัน Node.js ที่ติดตั้งอยู่
      - ถ้าไม่ได้ติดตั้ง: ไม่รองรับ (Not Supported)

- go
    Runtime: Go
    Process Provider: systemd (Linux) / PM2 (macOS, Windows)
    Entry Point: app [ไฟล์ binary ที่ build แล้ว] (ค่าเริ่มต้น)
    ปรับแต่งได้: ได้
    ตรวจสอบ Runtime:
      - แสดงเวอร์ชัน Go ที่ติดตั้งอยู่
      - ถ้าไม่ได้ติดตั้ง: ไม่รองรับ (Not Supported)

- python
    Runtime: Python
    Process Provider: systemd (Linux) / PM2 (macOS, Windows)
    Entry Point: app.py (ค่าเริ่มต้น)
    ปรับแต่งได้: ได้
    ตรวจสอบ Runtime:
      - แสดงเวอร์ชัน Python ที่ติดตั้งอยู่
      - ถ้าไม่ได้ติดตั้ง: ไม่รองรับ (Not Supported)

- php
    Runtime: PHP
    Process Provider: php-fpm
    Service Manager: php-fpm master ของระบบ (ค่าเริ่มต้นเดิม)
    Entry Point: public/index.php
    ปรับแต่งได้: ได้
    ตรวจสอบ Runtime:
      - แสดงเวอร์ชัน PHP ที่ติดตั้งอยู่
      - แสดงเวอร์ชัน PHP-FPM ที่ติดตั้งอยู่
      - ถ้าไม่ได้ติดตั้ง: ไม่รองรับ (Not Supported)
    Per-service pool (Linux/macOS เท่านั้น ดู DESIGN.md หัวข้อ 8):
      - php service แต่ละตัวจะได้ php-fpm pool เป็นของตัวเอง (socket
        เฉพาะของตัวเอง, เวอร์ชัน PHP-FPM ที่เลือกได้เอง) โดยอัตโนมัติ
        เมื่อเครื่องตรวจพบ PHP-FPM เพียงเวอร์ชันเดียว
        (`/etc/php/<version>/fpm` บน Linux, Homebrew (ทั้งฟอร์มูลาที่ตั้ง
        เวอร์ชันแบบ `php@<version>` และฟอร์มูลา `php` เฉย ๆ ที่เป็นเวอร์ชัน
        ล่าสุดโดยไม่มีการตั้งชื่อเวอร์ชัน) บน macOS)
        `--php-version=<version>` ใช้เลือกเวอร์ชันเมื่อตรวจพบหลายเวอร์ชัน
        พร้อมกัน ส่วน `--no-php-pool` คือกลับไปใช้ PHP_FPM_ENDPOINT แบบเดิม
        (ใช้ร่วมกันทั้งโฮสต์)
      - **ความเป็นเจ้าของ pool (unix user) ต่างกันตาม OS**: บน Linux
        (php-fpm master รันเป็น root ผ่าน systemd) pool แต่ละตัวมี unix
        user เฉพาะของตัวเอง (สร้างผ่าน `useradd --system --no-create-home`)
        แยกจากกันอย่างสมบูรณ์ระหว่าง service แต่ละตัว แต่บน **macOS
        (Homebrew) จะไม่แยก unix user แต่ละ pool อีกต่อไป** เพราะ
        php-fpm master ที่รันผ่าน `brew services` เป็น unprivileged
        process (รันเป็น login user ปกติ ไม่ใช่ root) จึงไม่มีสิทธิ์
        chown socket หรือสลับ worker ไปเป็นอีก user หนึ่งได้ -- pool
        บน macOS ทุกตัวจึงรันเป็น login user เดียวกับที่รัน php-fpm
        master นั่นเอง ไม่มีการแยกสิทธิ์ระหว่าง service บน macOS
        (พบและตัดสินใจแก้ 2026-07-05 หลังเจอ error จริงบนเครื่องที่ใช้งาน)
      - php service ที่มีอยู่ก่อนฟีเจอร์นี้จะไม่ถูก migrate อัตโนมัติ --
        ยังใช้ PHP_FPM_ENDPOINT ร่วมกันเหมือนเดิม จนกว่าจะสร้างใหม่ให้มี
        pool เฉพาะของตัวเอง
      - Windows ใช้ PHP_FPM_ENDPOINT ร่วมกันเสมอ -- PHP-FPM ไม่มีเวอร์ชัน
        official สำหรับ Windows เลยไม่มี pool ในเครื่องให้ wor จัดการ
