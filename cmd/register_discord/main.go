package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	appID := os.Getenv("DISCORD_APP_ID")
	token := os.Getenv("DISCORD_BOT_TOKEN")

	if appID == "" || token == "" {
		fmt.Println("==========================================================================")
		fmt.Println("❌ ERROR: Faltan credenciales.")
		fmt.Println("Para registrar el comando necesitas establecer las variables primero.")
		fmt.Println("\nFijate en tu Discord Developer Portal -> General Information (Application ID)")
		fmt.Println("y -> Bot (Token).")
		fmt.Println("\nLuego ejecuta esto en tu consola:")
		fmt.Println("$env:DISCORD_APP_ID=\"tu_application_id_aqui\"")
		fmt.Println("$env:DISCORD_BOT_TOKEN=\"tu_token_aqui\"")
		fmt.Println("go run cmd/register_discord/main.go")
		fmt.Println("==========================================================================")
		os.Exit(1)
	}

	payload := map[string]interface{}{
		"name":        "chat",
		"description": "Habla con la Inteligencia Artificial",
		"options": []map[string]interface{}{
			{
				"name":        "mensaje",
				"description": "El mensaje para enviarle a la IA",
				"type":        3, // String type
				"required":    true,
			},
		},
		// 0=Guild, 1=Bot DM, 2=Private Channel
		"contexts": []int{0, 1, 2}, 
		// 0=Guild Install, 1=User Install
		"integration_types": []int{0, 1}, 
	}

	b, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://discord.com/api/v10/applications/%s/commands", appID)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("Content-Type", "application/json")

	fmt.Println("Registrando comando /chat en Discord...")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("❌ Error haciendo request:", err)
		return
	}
	defer resp.Body.Close()
	
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		fmt.Println("✅ ¡Éxito! Comando /chat registrado correctamente en servidores y DMs.")
		fmt.Println("Ahora abre tu chat privado con el bot en Discord, escribe '/' y verás salir la opción 'chat'.")
	} else {
		fmt.Printf("❌ Error registrando comando (Status %d): %s\n", resp.StatusCode, string(body))
	}
}
