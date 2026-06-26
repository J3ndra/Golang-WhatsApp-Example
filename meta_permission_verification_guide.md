# Meta Graph API Permission Verification Guide

This guide documents the specific **curl commands** used to satisfy Meta's App Review testing requirements (as seen on the *Testing your use cases* dashboard). 

To publish your app and move it from **Development** to **Live Mode**, Meta requires you to make at least one successful Graph API call using each of the requested scopes/permissions.

---

## Prerequisites

Before running these commands, ensure you have:
1. Run the server locally and exposed it (e.g., using `ngrok`).
2. Loaded your environment variables from your `.env` file. You can load them directly into your bash terminal using:
   ```bash
   export $(grep -v '^#' .env | xargs)
   ```

---

## Permission Testing Cheatsheet

### 1. `whatsapp_business_messaging`
*   **Goal:** Prove you can send messages to users.
*   **Action:** Send a standard text message.
*   **Curl Command:**
    ```bash
    curl -X POST "https://graph.facebook.com/v22.0/${WHATSAPP_PHONE_NUMBER_ID}/messages" \
      -H "Authorization: Bearer ${WHATSAPP_ACCESS_TOKEN}" \
      -H "Content-Type: application/json" \
      -d '{
        "messaging_product": "whatsapp",
        "recipient_type": "individual",
        "to": "RECIPIENT_PHONE_NUMBER",
        "type": "text",
        "text": {
          "body": "Test message to verify whatsapp_business_messaging permission"
        }
      }'
    ```
    *(Replace `RECIPIENT_PHONE_NUMBER` with your verified test recipient phone number).*

---

### 2. `public_profile`
*   **Goal:** Retrieve basic profile information (such as ID, name, and picture).
*   **Action:** Query the `/me` endpoint with custom profile fields.
*   **Curl Command:**
    ```bash
    curl -s -X GET "https://graph.facebook.com/v22.0/me?fields=id,name,picture" \
      -H "Authorization: Bearer ${WHATSAPP_ACCESS_TOKEN}"
    ```

---

### 3. `business_management`
*   **Goal:** Query details about your Meta Business Suite configuration.
*   **Action:** List the Business Portfolios associated with the token.
*   **Curl Command:**
    ```bash
    curl -s -X GET "https://graph.facebook.com/v22.0/me/businesses" \
      -H "Authorization: Bearer ${WHATSAPP_ACCESS_TOKEN}"
    ```
    *(Note: Even if this returns an empty `{"data":[]}` list, it still counts as a successful API request utilizing the permission).*

---

### 4. `whatsapp_business_management`
*   **Goal:** Manage configuration, phone numbers, or assets in your WhatsApp Business Account (WABA).
*   **Action:** Query the WABA details or get the list of WABA phone numbers.
*   **Method A (If you know your WABA ID):**
    ```bash
    curl -s -X GET "https://graph.facebook.com/v22.0/{YOUR_15_DIGIT_WABA_ID}/phone_numbers" \
      -H "Authorization: Bearer ${WHATSAPP_ACCESS_TOKEN}"
    ```
*   **Method B (Directly using the Phone Number ID):**
    If you don't know your WABA ID, you can query specific phone configuration fields on your Phone Number ID node:
    ```bash
    curl -s -X GET "https://graph.facebook.com/v22.0/${WHATSAPP_PHONE_NUMBER_ID}?fields=display_phone_number,verified_name,status,quality_rating" \
      -H "Authorization: Bearer ${WHATSAPP_ACCESS_TOKEN}"
    ```

---

## Verifying the Status
After running each command and receiving a successful response (HTTP `200 OK` or valid JSON), go back to your **Meta Developer Portal > Testing** dashboard and refresh the page. The respective permission row should instantly update to **Completed**.
