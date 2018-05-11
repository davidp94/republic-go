package cal

import "github.com/republicprotocol/republic-go/order"

type RenLedger interface {
	OpenOrder(signature [65]byte, orderID order.ID) error
	WaitForOpenOrder(orderID order.ID) error

	CancelOrder(signature [65]byte, orderID order.ID) error
	WaitForCancelOrder(orderID order.ID) error
}
