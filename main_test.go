package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func pedir(t *testing.T, api http.Handler, metodo, ruta, cuerpo string, estado int, destino any) {
	t.Helper()
	peticion := httptest.NewRequest(metodo, ruta, strings.NewReader(cuerpo))
	respuesta := httptest.NewRecorder()
	api.ServeHTTP(respuesta, peticion)
	if respuesta.Code != estado {
		t.Fatalf("%s %s: HTTP %d, se esperaba %d: %s", metodo, ruta, respuesta.Code, estado, respuesta.Body)
	}
	if destino != nil {
		if err := json.Unmarshal(respuesta.Body.Bytes(), destino); err != nil {
			t.Fatal(err)
		}
	}
}

func abrirParaPrueba(t *testing.T, archivo string) *sql.DB {
	t.Helper()
	db, err := abrirBaseDeDatos(archivo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func comprobarPrecios(t *testing.T, api http.Handler, ruta string, precios ...int) {
	t.Helper()
	var historial []RegistroPrecio
	pedir(t, api, "GET", ruta+"/precios", "", 200, &historial)
	if len(historial) != len(precios) {
		t.Fatalf("Historial = %+v, se esperaban %v", historial, precios)
	}
	for i, registro := range historial {
		if registro.PrecioCRC != precios[i] || registro.Fecha.IsZero() {
			t.Fatalf("Registro %d incorrecto: %+v", i, registro)
		}
	}
}

func TestMigracionYPersistencia(t *testing.T) {
	archivo := filepath.ToSlash(filepath.Join(t.TempDir(), "legacy.db"))
	legacy, err := sql.Open("sqlite", "file:"+archivo)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	_, err = legacy.Exec(`
		CREATE TABLE motos (
			id INTEGER PRIMARY KEY AUTOINCREMENT, marca TEXT NOT NULL, modelo TEXT NOT NULL,
			anio INTEGER NOT NULL CHECK (anio > 0), kilometraje INTEGER NOT NULL CHECK (kilometraje >= 0),
			precio_crc INTEGER NOT NULL CHECK (precio_crc > 0)
		);
		CREATE TABLE precios (
			id INTEGER PRIMARY KEY, moto_id INTEGER NOT NULL REFERENCES motos(id) ON DELETE CASCADE,
			precio_crc INTEGER NOT NULL CHECK (precio_crc > 0), fecha TEXT NOT NULL
		);
		INSERT INTO motos VALUES (42, 'Honda', 'Hornet', 2020, 10000, 1200000);
		INSERT INTO precios VALUES (9, 42, 1300000, '2025-01-01T00:00:00Z'), (15, 42, 1200000, '2025-02-01T00:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	db := abrirParaPrueba(t, archivo)
	api := rutas(db)
	var moto Moto
	pedir(t, api, "GET", "/motos/42", "", 200, &moto)
	if moto != (Moto{ID: 42, Marca: "Honda", Modelo: "Hornet", Anio: 2020, Kilometraje: 10000, PrecioCRC: 1200000}) {
		t.Fatalf("Moto migrada incorrecta: %+v", moto)
	}
	comprobarPrecios(t, api, "/motos/42", 1300000, 1200000)
	pedir(t, api, "PATCH", "/motos/42", `{"anuncio_url":"https://example.com/hornet"}`, 200, nil)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db = abrirParaPrueba(t, archivo)
	api = rutas(db)
	pedir(t, api, "GET", "/motos/42", "", 200, &moto)
	if moto.AnuncioURL != "https://example.com/hornet" {
		t.Fatalf("No se conservó el enlace: %+v", moto)
	}
	comprobarPrecios(t, api, "/motos/42", 1300000, 1200000)
	var idPrimerPrecio int
	if err := db.QueryRow("SELECT MIN(id) FROM precios WHERE moto_id = 42").Scan(&idPrimerPrecio); err != nil || idPrimerPrecio != 9 {
		t.Fatalf("ID del precio inicial = %d, error = %v", idPrimerPrecio, err)
	}
	var nueva Moto
	pedir(t, api, "POST", "/motos", `{"marca":"Honda","modelo":"CB","anio":2024,"precio_crc":900000}`, 201, &nueva)
	if nueva.ID != 43 {
		t.Fatalf("ID siguiente = %d, se esperaba 43", nueva.ID)
	}
}

func TestCRUDYFiltro(t *testing.T) {
	db := abrirParaPrueba(t, filepath.ToSlash(filepath.Join(t.TempDir(), "nueva.db")))
	api := rutas(db)
	pedir(t, api, "POST", "/motos", `{"marca":"Honda","modelo":"CB","anio":2024,"kilometraje":12,"precio_crc":1200000,"anuncio_url":"http://example.com/cb"}`, 201, nil)
	pedir(t, api, "POST", "/motos", `{"marca":"Yamaha","modelo":"MT","anio":2024,"precio_crc":1500000,"anuncio_url":"javascript:alert(1)"}`, 400, nil)
	pedir(t, api, "POST", "/motos", `{"marca":"Yamaha","modelo":"MT","anio":2024,"precio_crc":1500000}`, 201, nil)

	var motos []Moto
	pedir(t, api, "GET", "/motos", "", 200, &motos)
	if len(motos) != 2 || motos[0].ID != 1 || motos[1].ID != 2 || motos[0].AnuncioURL != "http://example.com/cb" {
		t.Fatalf("Listado incorrecto: %+v", motos)
	}
	pedir(t, api, "GET", "/motos?precio_max=1200000", "", 200, &motos)
	if len(motos) != 1 || motos[0].ID != 1 {
		t.Fatalf("Filtro inclusivo incorrecto: %+v", motos)
	}
	pedir(t, api, "GET", "/motos?precio_max=0", "", 200, &motos)
	if len(motos) != 0 {
		t.Fatalf("Presupuesto cero devolvió %+v", motos)
	}
	pedir(t, api, "GET", "/motos?precio_max=-1", "", 400, nil)

	const ruta = "/motos/1"
	comprobarPrecios(t, api, ruta, 1200000)
	var editada Moto
	pedir(t, api, "PATCH", ruta, `{"kilometraje":0}`, 200, &editada)
	if editada.Kilometraje != 0 || editada.PrecioCRC != 1200000 || editada.AnuncioURL == "" {
		t.Fatalf("PATCH parcial incorrecto: %+v", editada)
	}
	comprobarPrecios(t, api, ruta, 1200000)

	pedir(t, api, "PATCH", ruta, `{"marca":"Kawasaki","modelo":"Z","anio":2025,"anuncio_url":"https://example.com/z"}`, 200, nil)
	pedir(t, api, "PATCH", ruta, `{"anuncio_url":"//example.com","kilometraje":18}`, 400, nil)
	editada = Moto{}
	pedir(t, api, "PATCH", ruta, `{"anuncio_url":""}`, 200, &editada)
	if editada.Marca != "Kawasaki" || editada.Modelo != "Z" || editada.Anio != 2025 || editada.Kilometraje != 0 || editada.AnuncioURL != "" {
		t.Fatalf("Edición o eliminación del enlace incorrecta: %+v", editada)
	}
	comprobarPrecios(t, api, ruta, 1200000)

	for i := 0; i < 2; i++ {
		pedir(t, api, "PATCH", ruta, `{"precio_crc":1100000}`, 200, nil)
	}
	comprobarPrecios(t, api, ruta, 1200000, 1100000)

	_, err := db.Exec(`CREATE TRIGGER fallar_precio BEFORE INSERT ON precios
		BEGIN SELECT RAISE(ABORT, 'fallo de prueba'); END`)
	if err != nil {
		t.Fatal(err)
	}
	pedir(t, api, "PATCH", ruta, `{"precio_crc":1000000,"kilometraje":4}`, 500, nil)
	pedir(t, api, "GET", ruta, "", 200, &editada)
	if editada.PrecioCRC != 1100000 || editada.Kilometraje != 0 {
		t.Fatalf("La edición no se revirtió: %+v", editada)
	}
	comprobarPrecios(t, api, ruta, 1200000, 1100000)

	pedir(t, api, "DELETE", ruta, "", 204, nil)
	pedir(t, api, "GET", ruta, "", 404, nil)
	pedir(t, api, "GET", ruta+"/precios", "", 404, nil)
	var restantes int
	if err := db.QueryRow("SELECT COUNT(*) FROM precios WHERE moto_id = 1").Scan(&restantes); err != nil || restantes != 0 {
		t.Fatalf("Quedaron %d precios de la moto eliminada, error = %v", restantes, err)
	}
}
