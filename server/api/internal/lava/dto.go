package lava

// --- CreateInvoice / GetInvoice / ListProducts request/response DTOs.
// RESEARCH.md §1.1 (CreateInvoiceRequest), §1.2 (InvoiceDetailResponse),
// §1.3 (ProductsResponse + ProductItemResponse), §1.5 (WebhookEvent shapes).

// CreateInvoiceRequest is POST /api/v3/invoice request body.
type CreateInvoiceRequest struct {
	Email           string            `json:"email"`
	OfferID         string            `json:"offerId"`
	Currency        string            `json:"currency"`
	Periodicity     string            `json:"periodicity,omitempty"`
	BuyerLanguage   string            `json:"buyerLanguage,omitempty"`
	PaymentProvider string            `json:"paymentProvider,omitempty"`
	PaymentMethod   string            `json:"paymentMethod,omitempty"`
	ClientUtm       map[string]string `json:"clientUtm,omitempty"`
	Amount          *float64          `json:"amount,omitempty"`
}

// InvoiceAmount is the amountTotal nested object.
type InvoiceAmount struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// InvoiceResponse is the POST /api/v3/invoice response (RESEARCH §1.1).
type InvoiceResponse struct {
	ID          string        `json:"id"`
	Status      string        `json:"status"`
	AmountTotal InvoiceAmount `json:"amountTotal"`
	PaymentURL  *string       `json:"paymentUrl"`
}

// InvoiceDetailResponse is the GET /api/v2/invoices/{id} response (RESEARCH §1.2).
// Used by the escalate path (D-25) in /api/v1/invoices/:id?escalate=true.
type InvoiceDetailResponse struct {
	ID                  string                      `json:"id"`
	Type                string                      `json:"type"`
	Datetime            string                      `json:"datetime"`
	Status              string                      `json:"status"`
	Receipt             InvoiceReceipt              `json:"receipt"`
	Buyer               InvoiceBuyer                `json:"buyer"`
	Product             InvoiceProduct              `json:"product"`
	ParentInvoice       *InvoiceParent              `json:"parentInvoice,omitempty"`
	SubscriptionStatus  *string                     `json:"subscriptionStatus,omitempty"`
	SubscriptionDetails *InvoiceSubscriptionDetails `json:"subscriptionDetails,omitempty"`
	ClientUtm           map[string]*string          `json:"clientUtm,omitempty"`
}

type InvoiceReceipt struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
	Fee      float64 `json:"fee"`
}

type InvoiceBuyer struct {
	Email    string `json:"email"`
	CardMask string `json:"cardMask"`
}

type InvoiceProduct struct {
	Name  string `json:"name"`
	Offer string `json:"offer"`
}

type InvoiceParent struct {
	ID string `json:"id"`
}

type InvoiceSubscriptionDetails struct {
	ExpiredAt    *string `json:"expiredAt"`
	TerminatedAt *string `json:"terminatedAt"`
	CancelledAt  *string `json:"cancelledAt"`
}

// ProductsResponse is GET /api/v2/products response (RESEARCH §1.3).
type ProductsResponse struct {
	Items    []ProductsItem `json:"items"`
	NextPage *string        `json:"nextPage,omitempty"`
}

type ProductsItem struct {
	Type string              `json:"type"` // "PRODUCT" or "POST"
	Data ProductItemResponse `json:"data"` // when type=PRODUCT
}

type ProductItemResponse struct {
	ID          string         `json:"id"`
	Title       *string        `json:"title"`
	Description *string        `json:"description"`
	Type        string         `json:"type"`
	Offers      []ProductOffer `json:"offers"`
}

type ProductOffer struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description *string             `json:"description"`
	Prices      []ProductOfferPrice `json:"prices"`
	Recurrent   bool                `json:"recurrent"`
}

type ProductOfferPrice struct {
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	Periodicity string  `json:"periodicity"`
}

// WebhookEvent is the inbound webhook payload (RESEARCH §1.5). The handler
// in plan 03-06 BodyParser-s into this; field optionality reflects which
// events carry which fields.
type WebhookEvent struct {
	EventType        string          `json:"eventType"`
	ContractID       string          `json:"contractId"`
	ParentContractID *string         `json:"parentContractId,omitempty"`
	Amount           *float64        `json:"amount,omitempty"`    // omitted on subscription.cancelled
	Currency         *string         `json:"currency,omitempty"`  // omitted on subscription.cancelled
	Timestamp        *string         `json:"timestamp,omitempty"` // omitted on subscription.cancelled (use CancelledAt)
	Status           *string         `json:"status,omitempty"`
	ErrorMessage     *string         `json:"errorMessage,omitempty"`
	Product          *WebhookProduct `json:"product,omitempty"`
	Buyer            *WebhookBuyer   `json:"buyer,omitempty"`
	CancelledAt      *string         `json:"cancelledAt,omitempty"`  // only on subscription.cancelled
	WillExpireAt     *string         `json:"willExpireAt,omitempty"` // only on subscription.cancelled
}

type WebhookProduct struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type WebhookBuyer struct {
	Email string `json:"email"`
}
