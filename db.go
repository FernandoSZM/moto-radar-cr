package main

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

func abrirBaseDeDatos(archivo string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+archivo+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		return nil, err
	}

	if err := migrar(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrar(db *sql.DB) error {
	const esquema = `
		CREATE TABLE IF NOT EXISTS motos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			marca TEXT NOT NULL,
			modelo TEXT NOT NULL,
			anio INTEGER NOT NULL CHECK (anio > 0),
			kilometraje INTEGER NOT NULL CHECK (kilometraje >= 0),
			precio_crc INTEGER NOT NULL CHECK (precio_crc > 0),
			anuncio_url TEXT NOT NULL DEFAULT ''
		);

		CREATE TABLE IF NOT EXISTS precios (
			id INTEGER PRIMARY KEY,
			moto_id INTEGER NOT NULL REFERENCES motos(id) ON DELETE CASCADE,
			precio_crc INTEGER NOT NULL CHECK (precio_crc > 0),
			fecha TEXT NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_precios_moto_id ON precios(moto_id);
	`

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(esquema); err != nil {
		return err
	}

	var tieneAnuncio bool
	err = tx.QueryRow(`SELECT EXISTS (
		SELECT 1 FROM pragma_table_info('motos') WHERE name = 'anuncio_url'
	)`).Scan(&tieneAnuncio)
	if err != nil {
		return err
	}
	if !tieneAnuncio {
		if _, err := tx.Exec(`ALTER TABLE motos ADD COLUMN anuncio_url TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return tx.Commit()
}
