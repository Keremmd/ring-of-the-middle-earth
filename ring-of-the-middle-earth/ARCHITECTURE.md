# Ring of the Middle Earth — Architecture (Özet)

> **Tam belge:** Tüm mimari diyagramlar, Kafka topolojileri, EventRouter, fault tolerance, paradigm justification, reflection ve LLM logu artık **[RAPOR_README.md](./RAPOR_README.md)** içinde **Bölüm B** olarak birleştirilmiştir.  
> Hocaya PDF sunarken doğrudan `RAPOR_README.md` dosyasını kullanın.

## Hızlı referans

| Konu | RAPOR_README bölümü |
|------|---------------------|
| Sistem diyagramı | §4 |
| Goroutine + kanallar | §5 |
| Kafka 10 topic + Topology 1/2 | §6 |
| EventRouter + WorldStateCache | §7 |
| Config-driven (no unit ID) | §8 |
| Consumer group / GameOver | §9 |
| Go vs Akka gerekçesi | §10 |
| Reflection (300+ kelime) | §11 |
| Görsel rehberi (rapor PDF) | §18 |

## Tek satırlık özet

**Option B:** Vanilla JS + SSE → Nginx → Go (goroutines) → Kafka (3 broker, 10 topic, Avro) → bilgi gizleme `EventRouter` ile; tur işleme 13 adım, 60 sn/tur, config-driven 14 birim.

```bash
make up    # http://localhost?playerId=light | dark
make test  # go test -race
```

---

*Eski tam ARCHITECTURE içeriği korunmuştur — bkz. [RAPOR_README.md](./RAPOR_README.md).*
