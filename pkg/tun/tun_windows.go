//go:build windows

package tun

import (
	"fmt"
	"net/netip"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

type windowsManager struct {
	proxyIP      netip.Addr
	gateway      netip.Addr
	tunName      string
	physicalName string

	tunLuid  winipcfg.LUID
	physLuid winipcfg.LUID
}

func (w *windowsManager) ensureInterface() error {
	// Запрашиваем у ОС список всех сетевых интерфейсов
	adapters, err := winipcfg.GetAdaptersAddresses(
		windows.AF_UNSPEC,                       // Семейство адресов (IPv4, IPv6)
		windows.GAA_FLAG_INCLUDE_ALL_INTERFACES, // вернуть вообще все адаптеры
	)
	if err != nil {
		return fmt.Errorf("failed to get network adapters: %w", err)
	}

	var tunGUID, physGUID *windows.GUID

	// Ищем GUID-ы в цикле по именам адаптеров

	for _, adapter := range adapters {
		friendlyName := adapter.FriendlyName()

		if friendlyName == w.tunName {
			// Переводим строковый AdapterName (который в Windows является GUID-ом) в *windows.GUID
			guid, err := windows.GUIDFromString(adapter.AdapterName())
			if err == nil {
				tunGUID = &guid
			}
		}

		if friendlyName == w.physicalName {
			guid, err := windows.GUIDFromString(adapter.AdapterName())
			if err == nil {
				physGUID = &guid
			}
		}
	}

	// Проверяем, что нашли оба интерфейса
	if tunGUID == nil {
		err = fmt.Errorf("tunnel interface '%s' not found", w.tunName)
		return err
	}
	if physGUID == nil {
		err = fmt.Errorf("physical interface '%s' not found", w.physicalName)
		return err
	}

	// ПОЛУЧАЕМ LUID ДЛЯ НАШЕГО VPN-ТУННЕЛЯ и ДЛЯ ФИЗИЧЕСКОЙ КАРТЫ
	w.tunLuid, err = winipcfg.LUIDFromGUID(tunGUID)
	if err != nil {
		err = fmt.Errorf("failed to get tunnel interface LUID: %w", err)
		return err
	}

	w.physLuid, err = winipcfg.LUIDFromGUID(physGUID)
	if err != nil {
		err = fmt.Errorf("failed to get physical interface LUID: %w", err)
		return err
	}
	return err
}

// Конструктор принимает только обычные строки! Никакой специфики Windows снаружи.
func newOSManager(proxyIPStr, gatewayStr, tunName, physName string) (tunManager, error) {
	proxyIP, err := netip.ParseAddr(proxyIPStr)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy IP: %w", err)
	}

	gateway, err := netip.ParseAddr(gatewayStr)
	if err != nil {
		return nil, fmt.Errorf("invalid gateway IP: %w", err)
	}

	return &windowsManager{
		proxyIP:      proxyIP,
		gateway:      gateway,
		tunName:      tunName,
		physicalName: physName,
	}, nil
}

func (m *windowsManager) SetupInterface(dnsStr string) error {

	// 1. Достаем LUID физ и вирт интерфейсов
	m.ensureInterface()

	// 2. НАЗНАЧАЕМ IP-АДРЕС ТУННЕЛЮ
	tunIP := netip.MustParsePrefix("10.0.0.2/24")
	if err := m.tunLuid.SetIPAddresses([]netip.Prefix{tunIP}); err != nil {
		return fmt.Errorf("failed to set IP address to tunnel: %w", err)
	}

	// 3. ДОБАВЛЯЕМ МАРШРУТ ДО ПРОКСИ-СЕРВЕРА
	proxyPrefix := netip.PrefixFrom(m.proxyIP, 32)

	if err := m.physLuid.AddRoute(proxyPrefix, m.gateway, 0); err != nil {
		return fmt.Errorf("failed to add proxy route: %w", err)
	}

	// 4. ЗАВЕРТЫВАЕМ ВЕСЬ ИНТЕРНЕТ В ТУННЕЛЕ (0.0.0.0/1 и 128.0.0.0/1)
	// Шлем эти маршруты ЧЕРЕЗ TUN LUID. В качестве nextHop передаем пустой IP (netip.Addr{}),
	// так как для интерфейса типа точка-точка (TUN) шлюз внутри туннеля не требуется.
	net1 := netip.MustParsePrefix("0.0.0.0/1")
	net2 := netip.MustParsePrefix("128.0.0.0/1")
	tunGateway := tunIP.Addr()

	if err := m.tunLuid.AddRoute(net1, tunGateway, 0); err != nil {
		return fmt.Errorf("failed to add 0.0.0.0/1 route: %w", err)
	}

	if err := m.tunLuid.AddRoute(net2, tunGateway, 0); err != nil {
		return fmt.Errorf("failed to add 128.0.0.0/1 route: %w", err)
	}

	// 5. УСТАНАВЛИВАЕМ DNS ДЛЯ VPN АДАПТЕРА
	dnsServer := netip.MustParseAddr(dnsStr)
	if err := m.tunLuid.SetDNS(windows.AF_INET, []netip.Addr{dnsServer}, nil); err != nil {
		return fmt.Errorf("failed to set DNS: %w", err)
	}

	return nil
}

func (m *windowsManager) Teardown() error {
	proxyPrefix := netip.PrefixFrom(m.proxyIP, 32)
	return m.physLuid.DeleteRoute(proxyPrefix, m.gateway)
}
