# Database

Access: Admin > Database

## 1. Overview

Gowiki supports structured data tables backed by PostgreSQL. This enables structured fields, queries, and data management beyond free-form wiki content.

## 1. Connecting

Configure the PostgreSQL DSN in Admin > Configuration > Database, then click **Connect**. The connection status is shown in Admin > Database.

## 1. Tables

Create tables with custom fields. Each table can be linked to wiki pages — rows are automatically synced when pages are saved.

## 1. Fields

Supported field types include text, number, date, select, and more. Fields can be added, modified, or archived (soft-deleted).

## 1. Data access

Table data is accessible via:
- The `{db-query}` directive in wiki pages
- The REST API (`/api/database/{table}/rows`)
- CSV export from the admin panel
