# twow-proxy

## Описание

Прокси-сервер для [Turtle WoW](https://turtlecraft.gg), написанный на Go. Предназначен для использования в случае отсутствия прямого доступа к auth и/или world серверам.

## Что понадобится

1. Базовые знания и навыки работы с Linux
2. VPS-сервер на Linux (Debian, Ubuntu), с которого есть прямой доступ к серверам Turtle WoW (см. ниже). Работа проверялась на Debian 13.
3. Если используете VPN — VPN-клиент с поддержкой раздельного туннелирования и умение его настроить, иначе смысл использования данного прокси теряется

## Проверка доступа

После аренды VPS-сервера первым делом проверяем, есть ли доступ к auth и world серверам (**обязательно** проверяем и то, и другое). Смотрим сначала, идёт ли пинг, затем — есть ли подключение по TCP. Если всё в порядке — можно приступать к деплою.

Что проверяем:

```
logon.turtle-server-eu.kz:3724 # Auth сервер
54.38.150.2:8090 # Ambershire
51.38.72.24:8090 # Nordanaar
51.38.75.18:8090 # Tel'Abim
```

### Пример для Ambershire

#### Проверка ping

```bash
ping 54.38.150.2 -c 8
```

Обращаем внимание на значение пинга (включая mdev) и packet loss (должен быть 0%).
```
PING 54.38.150.2 (54.38.150.2) 56(84) bytes of data.
64 bytes from 54.38.150.2: icmp_seq=1 ttl=49 time=69.1 ms
64 bytes from 54.38.150.2: icmp_seq=2 ttl=49 time=69.0 ms
64 bytes from 54.38.150.2: icmp_seq=3 ttl=49 time=69.0 ms
64 bytes from 54.38.150.2: icmp_seq=4 ttl=49 time=69.0 ms
64 bytes from 54.38.150.2: icmp_seq=5 ttl=49 time=69.0 ms
64 bytes from 54.38.150.2: icmp_seq=6 ttl=49 time=69.0 ms
64 bytes from 54.38.150.2: icmp_seq=7 ttl=49 time=69.0 ms
64 bytes from 54.38.150.2: icmp_seq=8 ttl=49 time=69.1 ms
--- 54.38.150.2 ping statistics ---
8 packets transmitted, 8 received, 0% packet loss, time 7007ms
rtt min/avg/max/mdev = 68.987/69.026/69.119/0.041 ms
```

#### Проверка доступности по TCP

```bash
nc -vz 54.38.150.2 8090
```

Ожидаемый ответ.

```
Connection to 54.38.150.2 8090 port [tcp/*] succeeded!
```

Повторюсь, проверить нужно **обязательно** и auth, и world сервер, на котором планируете играть.

## Деплой

В примере предполагается, что работаем не от рута, через sudo.

Скачиваем latest релиз из раздела [Releases](https://github.com/howaboutafuck/twow-proxy/releases) (копируем ссылку на файл twow-proxy, `wget <вставляем ссылку>` на сервере).

Должен появиться файл twow-proxy.

```
non-root-user@vps-server:~/turtle-proxy$ wget -q https://github.com/howaboutafuck/twow-proxy/releases/download/v0.0.1/twow-proxy
non-root-user@vps-server:~/turtle-proxy$ ls -l
total 4236
-rw-rw-r-- 1 non-root-user non-root-user 4334364 Apr 10 09:24 twow-proxy
```

Копируем файл в `/usr/local/bin/`.

```bash
sudo cp twow-proxy /usr/local/bin/
```

Делаем файл исполняемым.

```bash
sudo chmod +x /usr/local/bin/twow-proxy
```

Создаём директорию для хранения конфига.

```bash
sudo mkdir /usr/local/etc/twow-proxy
```

Открываем конфиг прокси...

```bash
sudo -e /usr/local/etc/twow-proxy/config.yaml
```

Откроется редактор (по умолчанию на Debian nano). Копируем туда содержимое `config.yaml` из папки `examples` репозитория. **ОБЯЗАТЕЛЬНО** меняем значение `listen_host` на внешний адрес VPS-сервера (узнать его можно в панели хостинга или командой `ip a`) и сохраняем файл.

Открываем service-файл...

```bash
sudo -e /etc/systemd/system/twow-proxy.service
```

И копируем туда содержимое `twow-proxy.service` из папки `examples` репозитория без каких-либо изменений.

Обновляем сервисы systemd.

```bash
sudo systemctl daemon-reload
```

Запускаем сервис.
```bash
sudo systemctl enable --now twow-proxy.service
```

Проверяем, что всё хорошо (`Active: active (running)`).
```bash
systemctl status twow-proxy.service
```
```
non-root-user@vps-server:~/turtle-proxy$ systemctl status twow-proxy.service
● twow-proxy.service - Turtle WoW Proxy
     Loaded: loaded (/etc/systemd/system/twow-proxy.service; enabled; preset: enabled)
     Active: active (running) since Fri 2026-04-10 09:54:15 UTC; 1min 24s ago
 Invocation: e58316d53cab4b5bbaca41b7c16b19dc
   Main PID: 103302 (twow-proxy)
      Tasks: 4 (limit: 2336)
     Memory: 4.4M (peak: 4.7M)
        CPU: 18ms
     CGroup: /system.slice/twow-proxy.service
             └─103302 /usr/local/bin/twow-proxy /usr/local/etc/twow-proxy/config.yaml

Apr 10 09:54:15 vps-server systemd[1]: Started twow-proxy.service - Turtle WoW Proxy.
Apr 10 09:54:15 vps-server twow-proxy[103302]: outbound 1.2.3.4
Apr 10 09:54:15 vps-server twow-proxy[103302]: world listen 1.2.3.4:8090 -> 54.38.150.2:8090 (Ambershire)
Apr 10 09:54:15 vps-server twow-proxy[103302]: world listen 1.2.3.4:8091 -> 51.38.72.24:8090 (Nordanaar)
Apr 10 09:54:15 vps-server twow-proxy[103302]: world listen 1.2.3.4:8092 -> 51.38.75.18:8090 (Tel'Abim)
Apr 10 09:54:15 vps-server twow-proxy[103302]: auth  listen 1.2.3.4:3724 -> logon.turtle-server-eu.kz:3724
```

На данном этапе прокси-сервер работает. Если на сервере используется firewall (рекомендуется), обязательно открыть порты 3724 и с 8090 по 8092 по TCP, либо те порты, которые указаны у Вас в конфиге прокси.

Чтобы посмотреть логи, используем `journalctl -u twow-proxy.service -f` (пользователь должен быть в группе `systemd-journal`, добавить можно с помощью `sudo adduser <username> systemd-journal`).

## Перенаправление Turtle WoW на прокси-сервер

Настройки realmlist у Turtle WoW перезаписываются лаунчером каждый запуск, а выставление read-only у `WTF/Config.wtf`, скорее всего, приведёт к невозможности менять настройки игры или другим проблемам.

Поэтому предлагается два способа.

### Файл hosts (простой способ)

Открываем Блокнотом от администратора файл `C:\Windows\System32\drivers\etc\hosts`, и вносим туда следующее (где 1.2.3.4 — IP Вашего сервера).

```
1.2.3.4 logon.turtle-server-eu.kz
```

И для очистки совести делаем в командной строке Windows `ipconfig /flushdns`.

В логах прокси-сервера должно отображаться Ваше подключение с IP вашего провайдера (не VPN!), когда заходите в игру.

### DNS-запись

Используем этот вариант, если:

- Есть доступ к роутеру в Вашей локальной сети
- Роутер поддерживает внесение DNS-записей
- У Вас несколько компов в локалке, и влом прописывать hosts

Не стоит забывать, что некоторые VPN-клиенты могут перехватывать DNS-запросы и заворачивать их через туннель куда-нибудь на `1.1.1.1`.

Гуглим, как добавить DNS-запись на `<модель роутера>`, добавляем A-запись для `logon.turtle-server-eu.kz` c IP Вашего VPS. Не забываем про сброс DNS-кэша.
