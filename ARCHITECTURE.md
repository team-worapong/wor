# WOR Architecture

## Overview

WOR (Web Runtime Manager) เป็น Runtime Manager สำหรับจัดการ Web Applications และ Runtime Environment

WOR ไม่ใช่

- Web Framework
- Package Manager
- Container Platform
- Process Manager

WOR มีหน้าที่เป็น Orchestrator ที่ควบคุม Runtime และ Infrastructure ผ่าน Interface เดียว

---

# Design Goals

Architecture ของ WOR ถูกออกแบบโดยมีเป้าหมายดังนี้

- Cross-platform
- Extensible
- Predictable
- Testable
- Minimal dependencies
- Clear separation of responsibilities
- Replaceable components

---

# Architecture Layers

                CLI
                 │
         Command Layer
                 │
              Engine
                 │
             Services
                 │
 Infrastructure Providers
                 │
      Platform Services
                 │
          Operating System

แต่ละ Layer ต้องรับผิดชอบหน้าที่ของตัวเองเท่านั้น

---

# Responsibilities

## Command Layer

รับคำสั่งจากผู้ใช้

เช่น

wor service start
wor host add
wor ssl issue

Command Layer มีหน้าที่

- Parse arguments
- Validate input
- Call Engine
- แสดงผล

Command Layer ห้ามมี Business Logic

---

## Engine

Engine เป็นตัวประสาน Workflow ของคำสั่ง

รับผิดชอบ

- Orchestration
- Workflow
- Coordination ระหว่าง Services
- Execute ขั้นตอนตามลำดับ

Engine ไม่ควรเข้าถึง Platform โดยตรง

---

## Services

Services เป็น Business Logic ของระบบ

รับผิดชอบ

- Validation
- Runtime Logic
- Domain Logic
- Configuration Logic
- Error Handling

Business Logic ทั้งหมดต้องอยู่ที่ Layer นี้

---

## Infrastructure Providers

Infrastructure Providers เชื่อมต่อกับ Software ภายนอก

แบ่งออกเป็น

Runtime Providers

- Node
- Go
- PHP
- Python

Process Providers

- PM2
- systemd
- launchd
- Windows Service

Web Server Providers

- Nginx
- Apache
- IIS

แต่ละ Provider ต้องเป็นอิสระจากกัน

---

## Platform Services

Platform Services จัดการความแตกต่างของแต่ละระบบปฏิบัติการ

ตัวอย่าง

- Filesystem
- Process
- Environment Variables
- Permissions
- Hosts File
- Symbolic Links
- Service Manager
- Path Separator

Command, Engine และ Services ไม่ควรตรวจสอบ runtime.GOOS โดยตรง

---

# Dependency Direction

Dependency ต้องไหลลงด้านล่างเสมอ

CLI

↓

Command

↓

Engine

↓

Services

↓

Infrastructure Providers

↓

Platform Services

↓

Operating System

Layer ด้านล่างห้ามอ้างอิง Layer ด้านบน

ห้ามเกิด Cyclic Dependency

---

# Configuration

Configuration มีลำดับดังนี้

1. Command Flags
2. Environment Variables
3. User Configuration
4. Default Values

การอ่าน Configuration ต้องผ่าน Config Package

---

# Output

Output ต้องผ่าน Output Package เท่านั้น

ห้ามใช้ fmt.Println() กระจายทั่วโปรเจกต์

Output Package ต้องสามารถรองรับ

- Plain Text
- JSON
- Table
- Color
- Future API

---

# Error Handling

ทุก Error ต้องถูกส่งกลับ

return error

ไม่ใช้ panic ยกเว้นกรณีที่ไม่สามารถทำงานต่อได้จริง

Command Layer เป็นผู้รับผิดชอบการแสดง Error แก่ผู้ใช้

---

# Cross-platform

ทุก Feature ต้องพิจารณา

- Linux
- macOS
- Windows

ตั้งแต่เริ่มออกแบบ

ห้ามออกแบบเฉพาะ Linux ก่อนแล้วค่อยแก้ทีหลัง

---

# Future Architecture

Foundation นี้จะรองรับการเพิ่ม

- Runtime Management
- Service Management
- Domain Management
- SSL Management
- Configuration Management
- Template System
- Deployment
- Monitoring
- Health Check
- Diagnostics
- Plugin System
- REST API
- Web Admin

โดยไม่ต้องเปลี่ยน Architecture หลัก

---

# Core Philosophy

WOR จัดการ Infrastructure

ไม่จัดการ Business

WOR ทำหน้าที่เป็น Orchestrator

ไม่ใช่ Runtime
ไม่ใช่ Web Server
ไม่ใช่ Application Framework

ทุก Component ต้องสามารถถูกแทนที่ (Replaceable)

ตัวอย่าง

Node → Bun

Apache → Nginx

PM2 → systemd

โดยไม่กระทบ Layer อื่น

---

# Design Principles

- Explicit over Implicit
- Predictable over Smart
- Safe over Convenient
- Composition over Inheritance
- Small Focused Packages
- Prefer Go Standard Library whenever possible
- Use Interfaces only when they provide real value
- Foundation before Features
