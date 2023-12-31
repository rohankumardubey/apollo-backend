package api

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/christianselig/apollo-backend/internal/domain"
	"github.com/christianselig/apollo-backend/internal/itunes"
)

func (a *api) checkReceiptHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	vars := mux.Vars(r)
	apns := vars["apns"]

	body, _ := ioutil.ReadAll(r.Body)
	iapr, err := itunes.NewIAPResponse(string(body), true)

	if err != nil {
		// treat as if it's a valid subscription, given that this is not the user's fault
		if apns != "" {
			dev, err := a.deviceRepo.GetByAPNSToken(ctx, apns)
			if err != nil {
				a.errorResponse(w, r, 500, err)
				return
			}

			dev.ExpiresAt = time.Now().Add(domain.DeviceActiveAfterReceitCheckDuration)
			dev.GracePeriodExpiresAt = dev.ExpiresAt.Add(domain.DeviceGracePeriodAfterReceiptExpiry)
			_ = a.deviceRepo.Update(ctx, &dev)
		}

		a.logger.Info("failed to verify receipt", zap.Error(err))
		a.errorResponse(w, r, 500, err)
		return
	}

	if apns != "" {
		dev, err := a.deviceRepo.GetByAPNSToken(ctx, apns)
		if err != nil {
			a.errorResponse(w, r, 500, err)
			return
		}

		if iapr.DeleteDevice {
			if dev.GracePeriodExpiresAt.Before(time.Now()) {
				accs, err := a.accountRepo.GetByAPNSToken(ctx, apns)
				if err != nil {
					a.errorResponse(w, r, 500, err)
					return
				}

				for _, acc := range accs {
					_ = a.accountRepo.Disassociate(ctx, &acc, &dev)
				}

				_ = a.deviceRepo.Delete(ctx, apns)
			}
		} else {
			dev.ExpiresAt = time.Now().Add(domain.DeviceActiveAfterReceitCheckDuration)
			dev.GracePeriodExpiresAt = dev.ExpiresAt.Add(domain.DeviceGracePeriodAfterReceiptExpiry)
			_ = a.deviceRepo.Update(ctx, &dev)
		}
	}

	w.WriteHeader(http.StatusOK)

	bb, _ := json.Marshal(iapr.VerificationInfo)
	_, _ = w.Write(bb)
}
