# WOR Architecture

## Overview

WOR (Web Runtime Manager) เป็น Runtime Manager สำหรับจัดการ Web Applications และ Runtime Environment

WOR ทำหน้าที่เป็น Infrastructure Orchestrator ที่รวมการจัดการ Runtime, Web Server และ Service Management ไว้ภายใต้ Interface เดียว

WOR ไม่ใช่

- Web Framework
- Package Manager
- Container Platform
- Process Manager
- Deployment Platform

WOR จัดการ Infrastructure

ไม่จัดการ Business Logic ของ Application

---

# Design Goals

Architecture ของ WOR ถูกออกแบบโดยมีเป้าหมายดังนี้

- Cross-platform
- Extensible
- Predictable
- Testable
- Minimal Dependencies
- Clear Separation of Responsibilities
- Replaceable Components
- Foundation Before Features

---

# Core Philosophy

WOR จัดการ Infrastructure

ไม่จัดการ Business

WOR ทำหน้าที่เป็น Orchestrator

ไม่ใช่ Runtime
ไม่ใช่ Web Server
ไม่ใช่ Application Framework

ทุก Component ต้องสามารถถูกแทนที่ได้

ตัวอย่าง

Node → Bun

Apache → Nginx

PM2 → systemd

โดยไม่กระทบ Layer อื่น

---

# Core Principles

Users interact with names.

The system operates on identifiers.

ผู้ใช้สื่อสารกับ WOR ผ่านชื่อ

ระบบทำงานด้วย Identifier ภายใน

Public Interface ต้องเรียบง่าย

Internal Implementation สามารถเปลี่ยนได้โดยไม่กระทบผู้ใช้

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

ตัวอย่าง

wor service add
wor host add
wor ssl issue

Command Layer มีหน้าที่

- Parse arguments
- Validate input
- Call Engine
- Render output

Command Layer ห้ามมี Business Logic

---

## Engine

Engine เป็นตัวประสาน Workflow

รับผิดชอบ

- Orchestration
- Workflow
- Coordination ระหว่าง Services
- Execute ขั้นตอนตามลำดับ

Engine ไม่ควรเข้าถึง Platform โดยตรง

---

## Services

Services เป็น Business Logic ของ WOR

รับผิดชอบ

- Validation
- Runtime Logic
- Configuration Logic
- Domain Logic
- Error Handling

Business Logic ทั้งหมดต้องอยู่ Layer นี้

---

## Infrastructure Providers

Infrastructure Providers เชื่อมต่อกับ Software ภายนอก

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

Platform Services จัดการความแตกต่างของระบบปฏิบัติการ

ตัวอย่าง

- Filesystem
- Environment
- Process
- Permissions
- Hosts File
- Symbolic Links
- Path Separator

Layer อื่นไม่ควรเรียก runtime.GOOS โดยตรง

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

ห้ามเกิด Cyclic Dependency

---

# Resource Model

WOR จัดการ Resource หลักดังนี้

Domain
- Root Domain ที่ WOR จัดการ

Service
- Application ภายใต้ Domain

Host
- Web Server Configuration ของ Service

SSL
- Certificate ของ Host

Deployment
- การ Deploy Service

ความสัมพันธ์

Domain
├── Services
├── Hosts
└── SSL

---

# Service Templates

Service Template เป็น Metadata ที่อธิบายลักษณะของ Service

Service Templates

- static
- static-node
- static-go
- static-python
- node
- go
- php
- python

Service Template กำหนด

- Runtime Requirements
- Process Requirements
- Default Application Route
- Directory Layout

Service Template เป็น Metadata

ไม่ใช่ Business Logic

---

# Runtime Requirements

Runtime Requirement ระบุ Software ที่จำเป็นสำหรับ Service Template

ตัวอย่าง

static
- none

node
- node
- npm

php
- php
- php-fpm

Runtime Validation เกิดตอนสร้าง Service

ไม่ใช่ตอน Setup

---

# Process Requirements

Process Requirement ระบุ Process Manager ที่ใช้

ตัวอย่าง

Node

- PM2

Go
- Platform Process Provider

Python
- Platform Process Provider

PHP
- Web Server + PHP-FPM

Platform Process Provider

Linux
- systemd

macOS
- launchd

Windows
- Windows Service

---

# Application Route

Service Template แบบ static-* รองรับ Application Route

Default

/app

Application Route เป็นคุณสมบัติของ Service

ไม่ใช่ของ WOR

ไม่ใช่ของ Service Template

---

# Domain Catalog

Domain Catalog คือรายการ Domain ที่ WOR จัดการ
Service ต้องอ้างอิง Domain จาก Domain Catalog
การจับคู่ Domain ต้องใช้ Longest Matching Domain
ห้ามเดา Root Domain จากจำนวน Labels

---

# Public vs Internal Identifiers

Public Identifier
ใช้ Fully Qualified Domain Name (FQDN)

ตัวอย่าง

example.com

app.example.com

api.app.example.com

CLI

Web Admin

REST API

SDK

ทั้งหมดต้องใช้ FQDN เป็นหลัก

Internal Identifier
- Domain ID
- Service ID

Identifier เหล่านี้เป็น Internal Implementation

ห้ามใช้เป็น Public CLI

ห้ามใช้เป็น Public API

ห้ามแสดงแก่ผู้ใช้โดยไม่จำเป็น

Architecture

User
↓
FQDN
↓
CLI / Web API
↓
Service Layer
↓
Domain ID
Service ID
↓
Metadata
↓
Filesystem

---

# Metadata

Metadata เป็น Source of Truth ของ WOR

ตัวอย่าง

- domain.json
- service.json
- host.json
- ssl.json

Metadata ควรมี Version

Filesystem เป็นเพียง Storage Implementation

---

# Service Lifecycle

Create
↓
Configure
↓
Host
↓
SSL
↓
Deploy
↓
Run
↓
Monitor
↓
Remove

---

# Configuration

ลำดับการอ่าน Configuration

1. Command Flags
2. Environment Variables
3. User Configuration
4. Default Values

การอ่าน Configuration ต้องผ่าน Config Package

---

# Output

Output ต้องผ่าน Output Package เท่านั้น

ต้องรองรับ

- Plain Text
- JSON
- Table
- Color

ห้ามใช้ fmt.Println() กระจายทั่วโปรเจกต์

---

# Error Handling

ทุก Error ต้องถูกส่งกลับ

return error

ไม่ใช้ panic ยกเว้นกรณีที่ไม่สามารถทำงานต่อได้จริง

Command Layer เป็นผู้แสดง Error ต่อผู้ใช้

---

# Cross-platform

ทุก Feature ต้องรองรับ

- Linux
- macOS
- Windows

ตั้งแต่เริ่มออกแบบ

ห้ามออกแบบเฉพาะ Linux ก่อนแล้วค่อยแก้

---

# Filesystem

Filesystem เป็น Storage Layer

ไม่ใช่ Public API

Directory Layout เป็น Internal Implementation

CLI และ API ต้องไม่อ้างอิง Directory Structure โดยตรง

---

# Future Architecture

Foundation นี้ต้องรองรับ

- Runtime Management
- Service Management
- Domain Management
- Host Management
- SSL Management
- Configuration Management
- Deployment
- Monitoring
- Health Check
- Diagnostics
- Plugin System
- REST API
- Web Admin

โดยไม่ต้องเปลี่ยน Architecture หลัก

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
