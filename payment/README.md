# Payment Service

A Go-based payment service that integrates Firebase Auth, LemonSqueezy payments, and DynamoDB for subscription management.

## Features

- **Firebase Authentication**: Secure user authentication using Firebase ID tokens
- **LemonSqueezy Integration**: Handle subscription checkouts and webhooks
- **DynamoDB Storage**: Persistent subscription data storage
- **CORS Support**: Configured for mayura.rocks domain
- **Webhook Processing**: Automatic subscription status updates
- **Health Checks**: Service health monitoring

## API Endpoints

### Authentication Required

All endpoints except `/health` and `/api/webhook` require Firebase Authentication.

Include the Firebase ID token in the Authorization header:
```
Authorization: Bearer <firebase_id_token>
```

### Endpoints

- **GET /health** - Health check endpoint
- **POST /api/checkout** - Create LemonSqueezy checkout session
- **GET /api/tier** - Get user's current subscription tier
- **GET /api/subscription** - Get detailed subscription information
- **POST /api/cancel-subscription** - Cancel user's subscription
- **POST /api/webhook** - LemonSqueezy webhook handler (signature verified)

## Request/Response Examples

### Create Checkout
```bash
POST /api/checkout
Authorization: Bearer <firebase_id_token>
Content-Type: application/json

{
  "tier": "plus",
  "variant_id": 123456  // optional, determined from tier if not provided
}
```

Response:
```json
{
  "checkout_url": "https://checkout.lemonsqueezy.com/...",
  "message": "Checkout created for plus tier"
}
```

### Get User Tier
```bash
GET /api/tier
Authorization: Bearer <firebase_id_token>
```

Response:
```json
{
  "tier": "plus",
  "status": "active",
  "expires_at": "2024-12-31T23:59:59Z",
  "user_id": "firebase_user_id"
}
```

## Environment Variables

### Required
- `LSZ_API_KEY` - LemonSqueezy API key
- `LSZ_STORE_ID` - LemonSqueezy store ID
- `FIREBASE_SERVICE_ACCOUNT_PATH` or `FIREBASE_SERVICE_ACCOUNT_JSON` - Firebase credentials

### Optional
- `PORT` - Server port (default: 8081)
- `GIN_MODE` - Gin mode (default: release)
- `DYNAMO_TABLE` - DynamoDB table name (default: subscriptions)
- `LSZ_WEBHOOK_SECRET` - LemonSqueezy webhook secret for signature verification
- `LSZ_REDIRECT_URL` - Redirect URL after successful payment
- `LSZ_RECEIPT_URL` - Receipt URL for customers
- `AWS_REGION` - AWS region (default: us-east-1)
- `AWS_ACCESS_KEY_ID` - AWS access key
- `AWS_SECRET_ACCESS_KEY` - AWS secret key

## DynamoDB Table Structure

Table name: `subscriptions`

**Partition Key**: `user_id` (String)

**Attributes**:
- `user_id` (S) - Firebase user ID
- `tier` (S) - Subscription tier (free, plus, pro)
- `variant_id` (N) - LemonSqueezy variant ID
- `status` (S) - Subscription status (active, cancelled, expired, etc.)
- `sub_id` (S) - LemonSqueezy subscription ID
- `created_at` (S) - Creation timestamp (RFC3339)
- `updated_at` (S) - Last update timestamp (RFC3339)
- `expires_at` (S) - Expiration timestamp (RFC3339, optional)
- `customer_id` (S) - LemonSqueezy customer ID (optional)
- `email` (S) - Customer email (optional)

## LemonSqueezy Configuration

### Variant IDs
Update the variant ID mappings in `lsz/client.go`:

```go
func GetVariantTier(variantID int) string {
    switch variantID {
    case 123456: // Replace with your actual Plus variant ID
        return "plus"
    case 123457: // Replace with your actual Pro variant ID
        return "pro"
    default:
        return "free"
    }
}
```

### Webhook Events
The service handles these LemonSqueezy webhook events:
- `subscription_created`
- `subscription_updated`
- `subscription_cancelled`
- `subscription_resumed`
- `subscription_expired`
- `subscription_paused`
- `subscription_unpaused`

## Docker Deployment

### Build and Run
```bash
# Build the service
docker build -t payment-service .

# Run the service
docker run -p 8081:8081 --env-file .env payment-service
```

### Docker Compose
The service is included in the main docker-compose.yml:
```bash
docker-compose up payment
```

## Development

### Prerequisites
- Go 1.23+
- Firebase project with service account
- LemonSqueezy account and API key
- AWS account with DynamoDB access

### Setup
1. Clone the repository
2. Copy `.env.example` to `.env` and fill in the values
3. Run the service:
   ```bash
   go run main.go
   ```

### Testing
```bash
# Health check
curl http://localhost:8081/health

# Create checkout (requires Firebase token)
curl -X POST http://localhost:8081/api/checkout \
  -H "Authorization: Bearer <firebase_token>" \
  -H "Content-Type: application/json" \
  -d '{"tier": "plus"}'
```

## Security

- Firebase ID tokens are verified for all authenticated endpoints
- LemonSqueezy webhook signatures are verified (if secret is configured)
- CORS is configured for specific domains
- No sensitive data is logged

## Logging

The service logs:
- Startup information
- Request/response details
- Error conditions
- Webhook processing results

## Error Handling

Common error responses:
- `401 Unauthorized` - Invalid or missing Firebase token
- `400 Bad Request` - Invalid request body or parameters
- `404 Not Found` - Resource not found
- `409 Conflict` - User already has subscription
- `500 Internal Server Error` - Server or database errors 