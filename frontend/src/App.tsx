import { Route, Routes } from 'react-router-dom'

function Home() {
  return (
    <main className="flex min-h-screen items-center justify-center">
      <div className="text-center">
        <h1 className="text-3xl font-bold text-gray-900">BudgetFlow</h1>
        <p className="mt-2 text-gray-600">Personal budgeting, coming together.</p>
      </div>
    </main>
  )
}

function App() {
  return (
    <Routes>
      <Route path="/" element={<Home />} />
    </Routes>
  )
}

export default App
