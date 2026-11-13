package server

// chat_prewarm.go — prewarm-пул личных чатов (карточка chat-prewarm-pool,
// хвост R1.3): заранее тёплая среда к моменту входа вместо «холодного»
// старта после TTL-сна. Подогрев идёт
//   - при логине (текущий пользователь — немедленно, асинхронно),
//   - по расписанию (каждые ChatPrewarmEvery — N последних активных
//     пользователей из журнала шлюза),
// и ограничен потолком одновременно тёплых рантаймов (ChatPrewarmN):
// «все зашли одновременно» не разгоняет пул сверх лимита.

import (
	"log"
	"time"
)

// prewarmMu защищает prewarmActive от перекрытий циклов подогрева
// (тикет расписания + логины); сам подогрев идёт последовательно.

// prewarmEnabled — выключен ли пул (0 = выкл, конфиг не трогаем рантаймом).
func (s *Server) prewarmEnabled() bool {
	return s.cfg.ChatPrewarmN > 0
}

// warmLimit — потолок одновременно тёплых рантаймов для пула.
func (s *Server) warmLimit() int {
	if s.cfg.ChatPrewarmN > 0 {
		return s.cfg.ChatPrewarmN
	}
	return 0
}

// warmCount — сколько рантаймов сейчас тёплые (в кэше чатов).
func (s *Server) warmCount() int {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	return len(s.chats)
}

// prewarmOnce — один цикл подогрева: добирает до лимита пула рантаймы
// самых недавних активных пользователей. Вызывается расписанием и логином
// (тогда candidates — сам вошедший пользователь первым в списке).
func (s *Server) prewarmOnce(extra []int64) {
	if !s.prewarmEnabled() {
		return
	}
	s.prewarmMu.Lock()
	if s.prewarmActive {
		s.prewarmMu.Unlock()
		return
	}
	s.prewarmActive = true
	s.prewarmMu.Unlock()
	defer func() {
		s.prewarmMu.Lock()
		s.prewarmActive = false
		s.prewarmMu.Unlock()
	}()

	recent, err := s.db.RecentChatUsers(s.warmLimit() + len(extra))
	if err != nil {
		log.Printf("[chat-prewarm] список активных пользователей недоступен: %v", err)
		recent = nil
	}
	seen := map[int64]bool{}
	order := make([]int64, 0, len(extra)+len(recent))
	for _, uid := range extra {
		if !seen[uid] {
			seen[uid] = true
			order = append(order, uid)
		}
	}
	for _, uid := range recent {
		if !seen[uid] {
			seen[uid] = true
			order = append(order, uid)
		}
	}
	for _, uid := range order {
		if s.warmCount() >= s.warmLimit() {
			return
		}
		s.chatMu.Lock()
		_, warm := s.chats[uid]
		s.chatMu.Unlock()
		if warm {
			continue
		}
		started := time.Now()
		if _, err := s.ensureChat(uid); err != nil {
			log.Printf("[chat-prewarm] пользователь %d: подогрев не удался: %v", uid, err)
			continue
		}
		log.Printf("[chat-prewarm] пользователь %d: среда подогрета за %s", uid, time.Since(started).Round(time.Millisecond))
	}
}

// prewarmScheduleLoop — тело goroutine расписания подогрева.
func (s *Server) prewarmScheduleLoop(tick <-chan time.Time) {
	for range tick {
		s.prewarmOnce(nil)
	}
}
