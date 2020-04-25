package test

const (
	// HybridOverlaySubnet is an annotation applied by the cluster network operator which is used by the hybrid overlay
	HybridOverlaySubnet = "k8s.ovn.org/hybrid-overlay-node-subnet"
	// HybridOverlayGatewayMAC is an annotation applied by the cluster network operator and used by the hybrid overlay
	HybridOverlayGatewayMAC = "k8s.ovn.org/hybrid-overlay-distributed-router-gateway-mac"
)
