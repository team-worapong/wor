คุณกำลังช่วยพัฒนาโปรเจกต์ WOR

WOR คือ Runtime Manager สำหรับ Web Applications ที่เขียนด้วย Go

หลักการสำคัญ:
- ออกแบบใหม่จากศูนย์
- ใช้ Go idiomatic design
- รองรับ Linux, macOS และ Windows
- รองรับ amd64 และ arm64
- Foundation ต้องสะอาด ขยายต่อได้ง่าย และดูแลระยะยาวได้
- Explicit ดีกว่า Implicit
- Predictable ดีกว่า Smart
- Safe ดีกว่า Convenient
- Foundation มาก่อน Features

WOR ไม่ใช่:
- Web Framework
- Package Manager
- Container Platform
- Auto Update Tool

แนวทาง Architecture:
- Command Layer ต้องบางที่สุด
- Business Logic ห้ามอยู่ใน command
- Business Logic ให้อยู่ใน internal/service หรือ internal/engine
- Platform-specific logic ให้อยู่ใน internal/platform เท่านั้น
- Platform package คือจุดเดียวที่สามารถเข้าถึง API เฉพาะของแต่ละ OS ได้
- โค้ดส่วนอื่นไม่ควรเรียก runtime.GOOS โดยตรง
- Configuration ต้องผ่าน config package
- หลีกเลี่ยง global mutable state
- หลีกเลี่ยง utility package ขนาดใหญ่
- ใช้ interface เมื่อมีประโยชน์จริงเท่านั้น
- คืน error แทน panic ยกเว้นกรณีที่ไม่สามารถ recover ได้
- Library และ service ห้ามพิมพ์ข้อความลง stdout โดยตรง
- Command layer เป็นผู้รับผิดชอบการแสดงผล

Dependency Rules:
- cmd สามารถเรียก internal ได้
- service/engine สามารถเรียก platform ได้
- platform ห้าม import service หรือ engine
- หลีกเลี่ยง cyclic dependency
- ออกแบบ dependency direction ให้เป็นทางเดียว

Testing:
- Business Logic ต้องสามารถทดสอบได้โดยไม่ต้องเรียก CLI
- Platform-specific code ต้องถูกแยกเพื่อให้ส่วนอื่นสามารถทดสอบได้
- Build และ Test ต้องผ่านเสมอ

ข้อห้ามถาวร:
- ห้ามทำ auto update
- ห้าม auto apply system changes
- ห้ามแก้ไฟล์ระบบโดยไม่ผ่าน explicit command
- ห้ามติดตั้ง package เองโดยไม่ขอ user confirmation
- ห้ามผูก project กับ Linux เพียงระบบเดียว

แนวทางการทำงาน:
- ก่อนแก้ไขโค้ด ให้สรุปสิ่งที่จะเปลี่ยนก่อน
- หากมีผลกระทบต่อ Architecture ให้เสนอทางเลือกก่อน
- แก้ไขทีละ Scope เล็ก
- หลังแก้ไข ให้สรุปไฟล์ที่เปลี่ยนและเหตุผล
- รักษา Build และ Test ให้ผ่านเสมอ
- หากเพิ่ม Command ใหม่ ต้องกำหนด Acceptance Criteria ให้ชัดเจน

Documentation Rules:
- AGENTS.md คือแนวทางการพัฒนา
- ARCHITECTURE.md คือ Architecture Specification หลัก
- ห้ามสร้างเอกสาร Architecture ซ้ำ
- หากต้องแก้ Architecture ให้แก้ที่ ARCHITECTURE.md
