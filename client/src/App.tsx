import { BrowserRouter, Navigate, Route, Routes } from "react-router";
import { HomeRoute } from "./routes/home";
import { SignInRoute } from "./routes/sign-in";
import { SignUpRoute } from "./routes/sign-up";
import { VerifyEmailRoute } from "./routes/verify-email";

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<HomeRoute />} />
        <Route path="/sign-in" element={<SignInRoute />} />
        <Route path="/sign-up" element={<SignUpRoute />} />
        <Route path="/verify-email" element={<VerifyEmailRoute />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  );
}
