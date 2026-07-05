# AGENTS.md

## Project Policy

เอกสารนี้เป็นนโยบายหลัก (Project Policy) สำหรับ AI Agent ทุกตัวที่ทำงานกับโปรเจกต์นี้

AI Agent ทุกตัวต้องอ่านและปฏิบัติตามเอกสารนี้ก่อนเริ่มทำงาน

หากข้อกำหนดในเอกสารนี้ขัดแย้งกับเอกสารอื่นในโปรเจกต์ ให้ยึด AGENTS.md เป็นหลัก

---

## Read Only Policy

ไฟล์ AGENTS.md เป็นไฟล์แบบ Read Only

AI Agent ไม่มีสิทธิ์

- แก้ไข
- เพิ่มข้อความ
- ลบข้อความ
- Rewrite
- Refactor
- เปลี่ยนโครงสร้าง
- สร้าง AGENTS.md ใหม่
- เขียนทับไฟล์นี้

มีเพียง Project Owner เท่านั้นที่สามารถแก้ไขไฟล์นี้ได้

---

## Project Information

Project Name:
WOR Runtime Manager

Development Language:
Go

Binary Output:

- Linux : wor
- macOS : wor
- Windows : wor.exe

---

## Project Identity

WOR คือ Runtime Manager
WOR ไม่ใช่ Hosting Panel
WOR ไม่ใช่ Web Control Panel
WOR มีหน้าที่จัดการเว็บไซต์หลายประเภทที่ใช้ Runtime แตกต่างกัน ผ่าน Command Line Interface (CLI)

ตัวอย่าง Runtime

- Static Website
- PHP
- Node.js
- Go
- Python

โดยแต่ละ Host สามารถเลือก Runtime ที่เหมาะสมกับการใช้งานได้อย่างอิสระ

---

## Project Vision

WOR มีเป้าหมายเป็น Runtime Management Platform

- รองรับหลาย Runtime
- รองรับหลาย Web Server
- รองรับหลายระบบปฏิบัติการ
- ใช้งานง่าย
- ดูแลรักษาง่าย
- ขยายระบบได้ในอนาคต

---

## Design Philosophy

ให้ยึดหลักการต่อไปนี้เสมอ

- Simplicity First
- Stability First
- Maintainability First
- Wizard First
- Explicit over Magic
- Predictable Behavior

---

## Engineering Principles

ทุกการพัฒนาควร

- อ่านโค้ดเดิมก่อน
- รักษาพฤติกรรมเดิมของระบบ
- เปลี่ยนเฉพาะส่วนที่ได้รับมอบหมาย
- หลีกเลี่ยงการ Rewrite ทั้งโปรเจกต์
- หลีกเลี่ยงการ Refactor ที่ไม่เกี่ยวข้อง
- ให้ความสำคัญกับความเสถียรมากกว่าการเพิ่ม Feature

---

## AI Working Rules

ก่อนเริ่มแก้ไข

1. ศึกษาโค้ดเดิม
2. วิเคราะห์ผลกระทบ
3. อธิบายแนวทางก่อนลงมือ หากมีผลต่อ Architecture หรือ Workflow
4. ดำเนินการแก้ไข
5. สรุปรายการเปลี่ยนแปลงหลังเสร็จงาน

---

## AI Decision Priority

เมื่อมีหลายแนวทางในการแก้ปัญหา ให้เรียงลำดับความสำคัญดังนี้

1. Correctness
2. Stability
3. Backward Compatibility
4. Simplicity
5. Maintainability
6. Performance
7. Developer Convenience

---

## Never Guess

AI Agent ต้องไม่เดา Requirement

หากข้อมูลไม่เพียงพอ

- ตรวจสอบโค้ดก่อน
- ตรวจสอบเอกสารใน docs/
- หากยังไม่ชัดเจน ให้สอบถาม Project Owner

ห้ามสร้าง Architecture ใหม่จากการคาดเดา

---

## Refactoring Policy

ห้าม Refactor โค้ดที่ไม่เกี่ยวข้องกับงาน
ห้ามเปลี่ยน Coding Style ทั้งโปรเจกต์
ห้าม Rewrite Module โดยไม่ได้รับคำสั่ง

---

## Backward Compatibility

หลีกเลี่ยงการทำให้โปรเจกต์เดิมใช้งานไม่ได้

หากมีแนวทางใหม่ที่ดีกว่า แต่ทำให้ระบบเดิมเสียหาย ให้เลือกแนวทางที่รองรับของเดิมก่อน และนำเสนอ Project Owner

---

## Safety Rules

ห้ามดำเนินการที่อาจทำให้ข้อมูลผู้ใช้สูญหายโดยไม่ได้รับการยืนยัน

เช่น

- ลบเว็บไซต์
- ลบ Configuration
- ลบ SSL Certificate
- ลบฐานข้อมูล
- ลบข้อมูลผู้ใช้

---

## Configuration Policy

Configuration ควร

- อ่านง่าย
- แก้ไขเองได้
- มีรูปแบบสม่ำเสมอ

Generated File ควรมี Header แจ้งว่าเป็นไฟล์ที่ระบบสร้าง

ห้ามเขียนทับ Configuration ของผู้ใช้โดยไม่แจ้งเตือน

---

## Runtime Principles

Runtime ต้องสามารถขยายได้ในอนาคต

ห้าม Hardcode Runtime ไว้ใน Core

รองรับ Runtime หลายประเภทผ่าน Provider หรือโครงสร้างที่สามารถขยายได้

---

## Web Server Principles

รองรับ Web Server หลายประเภท

เช่น

- Nginx
- Apache

Web Server Provider ควรแยกจาก Runtime Provider

---

## SSL Principles

รองรับ SSL Provider หลายประเภท

เช่น

- Let's Encrypt
- Self Signed

WOR ควรเป็นผู้จัดการ Configuration ของ Web Server เอง

---

## Cross Platform

WOR ต้องรองรับ

- Linux
- macOS
- Windows

Platform ใด ๆ ไม่ควรเป็นพลเมืองชั้นสอง

---

## Dependency Policy

ลดการใช้ Third-party Library ให้น้อยที่สุด

หากสามารถใช้ Go Standard Library ได้ ให้เลือกใช้ก่อน

---

## Logging Policy

Log ควรกระชับ

Error ควรอธิบาย

- เกิดอะไรขึ้น
- สาเหตุคืออะไร
- วิธีแก้ไขเบื้องต้น

---

## Documentation Policy

หากมีการเปลี่ยนแปลง

- Architecture
- Workflow
- Runtime
- Web Server
- Configuration

ให้แจ้งว่าควรอัปเดตเอกสารใน docs/ ที่เกี่ยวข้อง

---

## Out of Scope

AI Agent ไม่ควรเพิ่ม Feature ใหม่เอง

หากพบแนวทางที่ดีกว่า สามารถเสนอได้ แต่ไม่ควรดำเนินการเองโดยไม่ได้รับความเห็นชอบจาก Project Owner

---

## Reference Documents

รายละเอียดเชิงเทคนิคให้ศึกษาเพิ่มเติมจากเอกสารในโฟลเดอร์ docs/

เช่น

- PROJECT.md
- ARCHITECTURE.md
- RUNTIME.md
- WEBSERVER.md
- SSL.md
- CLI.md
- CODING_STYLE.md
- ROADMAP.md
