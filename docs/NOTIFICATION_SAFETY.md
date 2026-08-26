# Notification safety states

The normal Production and Notifications UI does not offer an indefinite pause or resume control. Delivery is available only when the saved Production configuration and all routing destinations are valid.

If a previously saved configuration has `Enabled=false`, KitsuSync preserves that record and fails closed. The normal UI presents it as requiring review, explains that delivery is unavailable, and directs the operator to review the notification configuration. It does not resume the configuration automatically and does not perform an external write.

The legacy pause/resume handler remains only for compatibility with previously stored records and controlled internal recovery procedures. It is not linked from the normal Dashboard or selected-Production Notifications view. Any future recovery action must validate the complete routing configuration before changing the stored state.
